package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"flag"
	"fmt"
	"github.com/go-logr/glogr"
	"github.com/go-logr/logr"
	"net/http"
	"os"
	"strings"
	"time"
	"regexp"

	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path/filepath"

	_ "github.com/lib/pq"
	"github.com/samedi/caldav-go"
	"github.com/samedi/caldav-go/data"
	"github.com/samedi/caldav-go/errs"
)

type MailConfig struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Config struct {
	DB   string     `yaml:"db"`
	Host string     `yaml:"host"`
	Mail MailConfig `yaml:"smtp"`
}

func Defaults() *Config {
	return &Config{
		DB:   "user=calendar database=calendar sslmode=disable",
		Host: "0.0.0.0:3000",
	}
}

type server struct {
	ctx context.Context
	db  *sql.DB
	log logr.Logger
}

type PGStorage struct {
	db     *sql.DB
	log    logr.Logger
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
	logr := ps.log.WithValues("haveAccess()", "PGStorage")

	var rows *sql.Rows
	var err error
	rows, err = ps.db.Query("SELECT permission FROM collection_role JOIN users ON collection_role.user_id = users.id JOIN collection ON collection_role.collection_id = collection.id  WHERE collection.name = $1 AND users.id = $2", getCollection(rpath), ps.UserID)
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
	logr := ps.log.WithValues("GetResources()", "PGStorage")
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
	rows, err = ps.db.Query("SELECT rpath FROM calendar WHERE rpath = $1 AND owner_id = $2 ", rpath, ps.UserID)
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
		res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
		result = append(result, res)
	}
	if isCollection(rpath) {
		res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
		result = append(result, res)
	}
	if withChildren && isCollection(rpath) {
		rows, err = ps.db.Query("SELECT rpath FROM calendar WHERE owner_id = $1", ps.UserID)
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
			res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.db, resourcePath: rrpath, log: ps.log.WithValues("PGResourceAdapter"), UserID: ps.UserID})
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
	logr := ps.log.WithValues("CreateResource()", "PGStorage")
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
	stmt, err := ps.db.Prepare("INSERT INTO calendar (rpath, content, owner_id) VALUES ($1, $2, $3)")
	if err != nil {
		logr.Error(err, "failed to prepare insert statement")
		return nil, err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(rpath, base64.StdEncoding.EncodeToString([]byte(content)), ps.UserID); err != nil {
		logr.Error(err, "failed to insert ", rpath)
		return nil, err
	}
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log, UserID: ps.UserID})
	logr.V(7).Info("resource created ", rpath)
	return &res, nil
}

func (ps *PGStorage) UpdateResource(rpath, content string) (*data.Resource, error) {
	logr := ps.log.WithValues("UpdateResource()", "PGStorage")
	a, err := ps.haveAccess(rpath, "write")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return nil, err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil, nil
	}
	stmt, err := ps.db.Prepare("UPDATE calendar SET content = $2, modified = $3 WHERE rpath = $1")
	if err != nil {
		logr.Error(err, "failed to prepare update statement ", rpath)
		return nil, err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(rpath, base64.StdEncoding.EncodeToString([]byte(content)), time.Now()); err != nil {
		logr.Error(err, "failed to update ", rpath)
		return nil, err
	}
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log, UserID: ps.UserID})
	logr.V(7).Info("resource updated ", rpath)
	return &res, nil
}

func (ps *PGStorage) DeleteResource(rpath string) error {
	logr := ps.log.WithValues("DeleteResource()", "PGStorage")
	a, err := ps.haveAccess(rpath, "admin")
	if err != nil {
		logr.Error(err, "failed to get Access [" + rpath + "]")
		return  err
	}
	if ! a {
		logr.Info("no access to collection [" + rpath + "]")
		return nil
	}
	_, err = ps.db.Exec("DELETE FROM calendar WHERE rpath = $1 AND owner_id = $2", rpath, ps.UserID)
	if err != nil {
		logr.Info("failed to delete resource ", rpath, " ", err.Error())
		return err
	}
	return nil
}

func (ps *PGStorage) isResourcePresent(rpath string) bool {
	rows, err := ps.db.Query("SELECT rpath FROM calendar WHERE rpath = $1 AND owner_id = $2", rpath, ps.UserID)
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

func (s *server) myHandler(w http.ResponseWriter, r *http.Request) {
	s.log.V(3).Info(fmt.Sprintf("%v", r))

	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)

	str := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(str) != 2 {
		http.Error(w, "Not authorized", 401)
		return
	}

	b, err := base64.StdEncoding.DecodeString(str[1])
	if err != nil {
		http.Error(w, err.Error(), 401)
		return
	}

	pair := strings.SplitN(string(b), ":", 2)
	if len(pair) != 2 {
		http.Error(w, "Not authorized", 401)
		return
	}

	var username, email string
	var id int64
	rows, err := s.db.Query("SELECT id, username, email FROM users WHERE username = $1 AND password = crypt($2, password)", pair[0], pair[1])
	if err != nil {
		s.log.Error(err, "failed to fetch user[", pair[0], "] password")
		http.Error(w, "user fetch error", 500)
		return
	}
	for rows.Next() {
		err = rows.Scan(&id, &username, &email)
		if err != nil {
			s.log.Error(err, "failed to fetch user[", pair[0], "] password")
			http.Error(w, "user fetch error", 500)
			return
		}
	}
	if id == 0 {
		http.Error(w, "Not authorized", 403)
		return
	}
	stg       := new(PGStorage)
	stg.db     = s.db
	stg.log    = s.log
	stg.User   = username
	stg.UserID = id
	stg.Email  = email

	caldav.SetupStorage(stg)
	// log.Printf("%v\n", request.Body)
	response := caldav.HandleRequest(r)
	s.log.V(3).Info(fmt.Sprintf("%v", response))
	// log.Printf("%v\n", response)
	// ... do something with the response object before writing it back to the client ...
	response.Write(w)
}

func (s *server) setupDB() error {
	logr := s.log.WithValues("DBSetup")
	if err := s.db.Ping(); err != nil {
		logr.Error(err, "failed to connect to DB, is it running")
		return err
	}
	_, err := s.db.Query("SELECT * FROM calendar LIMIT 1")
	if err == nil {
		return nil
	}
	schema, err := Asset("sql/calendar.sql")
	if err != nil {
		logr.Error(err, "failed to unpack sql template [sql/calendar.sql]")
		return err
	}
	tx, err := s.db.BeginTx(s.ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		logr.Error(err, "failed to begin DB transaction")
		return err
	}
	_, err = tx.Exec(string(schema))
	if err != nil {
		_ = tx.Rollback()
		logr.Error(err, "rolling back TX")
		return err
	}
	if err := tx.Commit(); err != nil {
		logr.Error(err, "failed to commit")
		return err
	}

	return nil
}

func main() {
	var confFlag string
	flag.StringVar(&confFlag, "conf", "julius.conf", "config file path")
	flag.Parse()

	log := glogr.New().WithName("Julius")

	conf := Defaults()

	if confFlag == "julius.conf" {
		_, err := os.Stat(confFlag)
		if err == nil {
			filename, err := filepath.Abs(confFlag)
			if err != nil {
				log.Error(err, "failed to read conf")
				os.Exit(1)
			}
			yamlFile, err := ioutil.ReadFile(filename)
			if err != nil {
				log.Error(err, "failed to read conf")
				os.Exit(1)
			}
			err = yaml.Unmarshal(yamlFile, &conf)
			if err != nil {
				log.Error(err, "failed to read conf")
				os.Exit(1)
			}
		}
	} else {
		filename, err := filepath.Abs(confFlag)
		if err != nil {
			log.Error(err, "failed to read conf")
			os.Exit(1)
		}
		yamlFile, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Error(err, "failed to read conf")
			os.Exit(1)
		}
		err = yaml.Unmarshal(yamlFile, &conf)
		if err != nil {
			log.Error(err, "failed to read conf")
			os.Exit(1)
		}
	}

	db, err := sql.Open("postgres", conf.DB)
	if err != nil {
		log.Error(err, "failed to open db")
		os.Exit(1)
	}
	s := server{db: db, log: log, ctx: context.Background()}
	if err = s.setupDB(); err != nil {
		os.Exit(1)
	}
	defer db.Close()
	log.V(2).Info("starting")
	http.HandleFunc("/", s.myHandler)
	http.ListenAndServe(conf.Host, nil)
}
