package storage

import (
	"fmt"
	"encoding/base64"
	"database/sql"
	"github.com/go-logr/logr"
	"time"
	"regexp"

	_ "github.com/lib/pq"
	"github.com/samedi/caldav-go/data"
	"github.com/samedi/caldav-go/errs"
)

type PGStorage struct {
	DB     *sql.DB
	Log    logr.Logger
	UserID int64
	Email  string
	User   string
}

func (ps *PGStorage) GetResourcesByList(rpaths []string) ([]data.Resource, error) {
	results := []data.Resource{}

	for _, rpath := range rpaths {
		resource, found, err := ps.GetShallowResource(rpath)

		if err != nil && err != errs.ResourceNotFoundError {
			return nil, err
		}

		if found {
			results = append(results, *resource)
		}
	}

	return results, nil
}

func isCollection(rpath string) bool {
	if rpath[len(rpath) - 1 : ] == "/" {
		return true
	}
	if rpath[len(rpath) - 3 : ] != "ics" {
		return true
	}
	return false
}

func (ps *PGStorage) haveAccess(rpath string, perm string) (bool, error) {
	logr := ps.Log.WithValues("haveAccess()", "PGStorage")

	var rows *sql.Rows
	var err error
	rows, err = ps.DB.Query("SELECT permission FROM collection_role JOIN users ON collection_role.user_id = users.id JOIN collection ON collection_role.collection_id = collection.id  WHERE collection.name = $1 AND users.id = $2", getCollection(rpath), ps.UserID)
	if err != nil {
		logr.Error(err, "failed to fetch permissions for " + rpath)
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var rowPerm string
		err := rows.Scan(&perm)
		if err != nil {
			logr.Error(err, "failed to scan rows")
			return false, err
		}
		if rowPerm == "admin" {
			return true, nil
		}
		if rowPerm == "write" && perm == "admin" {
			return false, nil
		} else {
			return true, nil
		}
		if rowPerm == "read" && perm == "read" {
			return true, nil
		} else {
			return false, nil
		}
	}

	return false, nil
}


var regex = regexp.MustCompile(`/[A-Za-z0-9-%@\.]*\.ics`)
func getCollection(rpath string) string {
	replace := regex.ReplaceAll([]byte(rpath), []byte("/"))
	return string(replace)
}

func (ps *PGStorage) GetResources(rpath string, withChildren bool) ([]data.Resource, error) {
	logr := ps.Log.WithValues("GetResources()", "PGStorage")
	logr.V(5).Info("Getting " + rpath)
	result := []data.Resource{}

	a, err := ps.haveAccess(rpath, "read")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return nil, err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil, nil
	}
	var rows *sql.Rows
	rows, err = ps.DB.Query("SELECT rpath FROM calendar WHERE rpath = $1 AND owner_id = $2 ", rpath, ps.UserID)
	if err != nil {
		logr.Error(err, "failed to fetch rpath")
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var rrpath string
		err := rows.Scan(&rrpath)
		if err != nil {
			logr.Error(err, "failed to scan rows")
			return nil, err
		}
		res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.DB, resourcePath: rpath, log: ps.Log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
		result = append(result, res)
	}
	if isCollection(rpath) {
		res := data.NewResource(rpath, &PGResourceAdapter{db: ps.DB, resourcePath: rpath, log: ps.Log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
		result = append(result, res)
	}
	if withChildren && isCollection(rpath) {
		rows, err = ps.DB.Query("SELECT rpath FROM calendar WHERE owner_id = $1", ps.UserID)
		if err != nil {
			logr.Error(err, "failed to scan rows")
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var rrpath string
			err := rows.Scan(&rrpath)
			if err != nil {
				logr.Error(err, "failed to scan rows")
				return nil, err
			}
			res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.DB, resourcePath: rrpath, log: ps.Log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
			result = append(result, res)
		}
	}

	return result, nil
}

func (ps *PGStorage) GetResourcesByFilters(rpath string, filters *data.ResourceFilter) ([]data.Resource, error) {
	result := []data.Resource{}

	//childPaths := fs.getDirectoryChildPaths(rpath)
	res, err := ps.GetResources("/", true)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		// only add it if the resource matches the filters
		if filters == nil || filters.Match(&r) {
			result = append(result, r)
		}
	}

	return result, nil
}

func (ps *PGStorage) GetResource(rpath string) (*data.Resource, bool, error) {
	return ps.GetShallowResource(rpath)
}

func (ps *PGStorage) GetShallowResource(rpath string) (*data.Resource, bool, error) {
	resources, err := ps.GetResources(rpath, false)

	if err != nil {
		return nil, false, err
	}

	if resources == nil || len(resources) == 0 {
		return nil, false, errs.ResourceNotFoundError
	}

	res := resources[0]
	return &res, true, nil
}

func (ps *PGStorage) CreateResource(rpath, content string) (*data.Resource, error) {
	logr := ps.Log.WithValues("CreateResource()", "PGStorage")
	logr.V(5).Info("Creating " + rpath)
	a, err := ps.haveAccess(rpath, "write")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return nil, err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil, nil
	}
	stmt, err := ps.DB.Prepare("INSERT INTO calendar (rpath, content, owner_id) VALUES ($1, $2, $3)")
	if err != nil {
		logr.Error(err, "failed to prepare insert statement")
		return nil, err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(rpath, base64.StdEncoding.EncodeToString([]byte(content)), ps.UserID); err != nil {
		logr.Error(err, "failed to insert ", rpath)
		return nil, err
	}
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.DB, resourcePath: rpath, log: ps.Log, UserID: ps.UserID})
	logr.V(7).Info("resource created ", rpath)
	return &res, nil
}

func (ps *PGStorage) UpdateResource(rpath, content string) (*data.Resource, error) {
	logr := ps.Log.WithValues("UpdateResource()", "PGStorage")
	a, err := ps.haveAccess(rpath, "write")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return nil, err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil, nil
	}
	stmt, err := ps.DB.Prepare("UPDATE calendar SET content = $2, modified = $3 WHERE rpath = $1")
	if err != nil {
		logr.Error(err, "failed to prepare update statement ", rpath)
		return nil, err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(rpath, base64.StdEncoding.EncodeToString([]byte(content)), time.Now()); err != nil {
		logr.Error(err, "failed to update ", rpath)
		return nil, err
	}
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.DB, resourcePath: rpath, log: ps.Log, UserID: ps.UserID})
	logr.V(7).Info("resource updated ", rpath)
	return &res, nil
}

func (ps *PGStorage) DeleteResource(rpath string) error {
	logr := ps.Log.WithValues("DeleteResource()", "PGStorage")
	a, err := ps.haveAccess(rpath, "admin")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return  err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil
	}
	_, err = ps.DB.Exec("DELETE FROM calendar WHERE rpath = $1 AND owner_id = $2", rpath, ps.UserID)
	if err != nil {
		logr.Info("failed to delete resource ", rpath, " ", err.Error())
		return err
	}
	return nil
}

func (ps *PGStorage) isResourcePresent(rpath string) bool {
	rows, err := ps.DB.Query("SELECT rpath FROM calendar WHERE rpath = $1 AND owner_id = $2", rpath, ps.UserID)
	if err != nil {
		return false
	}
	defer rows.Close()
	var rrpath string
	for rows.Next() {
		err = rows.Scan(&rrpath)
		if err != nil {
			return false
		}
		if rrpath == rpath {
			return true
		}
	}
	return false
}

type PGResourceAdapter struct {
	db           *sql.DB
	resourcePath string
	log          logr.Logger
	UserID       int64
}

func (pa *PGResourceAdapter) CalculateEtag() string {
	if pa.IsCollection() {
		return ""
	}

	return fmt.Sprintf(`"%x%x"`, pa.GetContentSize(), pa.GetModTime().UnixNano())
}

func (pa *PGResourceAdapter) haveAccess(perm string) (bool, error) {
	logr := pa.log.WithValues("haveAccess()")

	var rows *sql.Rows
	var err error
	rows, err = pa.db.Query("SELECT permission FROM collection_role JOIN users ON collection_role.user_id = users.id JOIN collection ON collection_role.collection_id = collection.id  WHERE collection.name = $1 AND users.id = $2", getCollection(pa.resourcePath), pa.UserID)
	if err != nil {
		logr.Error(err, "failed to fetch permissions for " + pa.resourcePath)
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var rowPerm string
		err := rows.Scan(&perm)
		if err != nil {
			logr.Error(err, "failed to scan rows")
			return false, err
		}
		if rowPerm == "admin" {
			return true, nil
		}
		if rowPerm == "write" && perm == "admin" {
			return false, nil
		} else {
			return true, nil
		}
		if rowPerm == "read" && perm == "read" {
			return true, nil
		} else {
			return false, nil
		}
	}

	return false, nil
}


func (pa *PGResourceAdapter) GetContent() string {
	logr := pa.log.WithValues("GetContent()")
	if pa.IsCollection() {
		return ""
	}
	a, err := pa.haveAccess("read")
	if err != nil {
		logr.Error(err, "failed to get Access ", pa.resourcePath)
		return ""
	}
	if ! a {
		return ""
	}
	rows, err := pa.db.Query("SELECT content FROM calendar WHERE rpath = $1 AND owner_id = $2", pa.resourcePath, pa.UserID)
	if err != nil {
		logr.Error(err, "failed to fetch content ", pa.resourcePath)
		return ""
	}
	defer rows.Close()
	var content string
	for rows.Next() {
		err = rows.Scan(&content)
		if err != nil {
			logr.Error(err, "failed to scan ", pa.resourcePath)
			return ""
		}
	}
	ret, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		logr.Error(err, "decode error ", pa.resourcePath)
		return ""
	}
	return string(ret)
}

func (pa *PGResourceAdapter) GetContentSize() int64 {
	return int64(len(pa.GetContent()))
}

func (pa *PGResourceAdapter) IsCollection() bool {
	return isCollection(pa.resourcePath)
}

func (pa *PGResourceAdapter) GetModTime() time.Time {
	logr := pa.log.WithValues("GetModTime()")
	a, err := pa.haveAccess("read")
	if err != nil {
		logr.Error(err, "failed to get Access ", pa.resourcePath)
		return time.Unix(0, 0)
	}
	if ! a {
		logr.Info("failed to get Access ", pa.resourcePath)
		return time.Unix(0, 0)
	}
	rows, err := pa.db.Query("SELECT modified FROM calendar WHERE rpath = $1 AND owner_id = $2", pa.resourcePath, pa.UserID)
	if err != nil {
		logr.Error(err, "failed to fetch modTime ", pa.resourcePath)
		return time.Unix(0, 0)
	}
	defer rows.Close()
	var mod time.Time
	for rows.Next() {
		err = rows.Scan(&mod)
		if err != nil {
			logr.Error(err, "failed to scan modTime ", pa.resourcePath)
			return time.Unix(0, 0)
		}
	}
	return mod
}

