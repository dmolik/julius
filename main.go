package main

import (
	"context"
	"os"
	"net/http"
	"database/sql"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/go-logr/glogr"
	"time"
	"encoding/base64"
	"flag"

	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path/filepath"

	_ "github.com/lib/pq"
	"github.com/samedi/caldav-go"
	"github.com/samedi/caldav-go/data"
	"github.com/samedi/caldav-go/errs"
)

type Config struct {
	DB   string `yaml:"db"`
	Host string `yaml:"host"`
}

func Defaults() *Config {
	return &Config{
		DB:   "user=calendar database=calendar sslmode=disable",
		Host: "0.0.0.0:3000",
	}
}

type server struct {
	ctx context.Context
	db *sql.DB
	log logr.Logger
}

type PGStorage struct {
	db *sql.DB
	log logr.Logger
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

func hasChildren(rpath string) bool {
	if rpath == "/" {
		return true
	}
	return false
}

func (ps *PGStorage) GetResources(rpath string, withChildren bool) ([]data.Resource, error) {
	logr := ps.log.WithValues("GetResources()", "PGStorage")
	result := []data.Resource{}

	var rows *sql.Rows
	var err error
	rows, err = ps.db.Query("SELECT rpath FROM calendar WHERE rpath = $1", rpath)
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
		res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log.WithValues("PGResourceAdapter")})
		result = append(result, res)
	}
	if hasChildren(rpath) {
		res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log.WithValues("PGResourceAdapter")})
		result = append(result, res)
	}
	if withChildren && hasChildren(rpath) {
		rows, err = ps.db.Query("SELECT rpath FROM calendar")
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
			res := data.NewResource(rrpath, &PGResourceAdapter{db: ps.db, resourcePath: rrpath, log: ps.log.WithValues("PGResourceAdapter")})
			result = append(result, res)
		}
	}

	return result, nil
}

func (ps *PGStorage) GetResourcesByFilters(rpath string, filters *data.ResourceFilter) ([]data.Resource, error) {
	result := []data.Resource{}

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
	stmt, err := ps.db.Prepare("INSERT INTO calendar (rpath, content) VALUES ($1, $2)")
	if err != nil {
		logr.Error(err, "failed to prepare insert statement")
		return nil, err
	}
	defer stmt.Close()
	if _, err := stmt.Exec(rpath, base64.StdEncoding.EncodeToString([]byte(content))); err != nil {
		logr.Error(err, "failed to insert ", rpath)
		return nil, err
	}
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log})
	logr.V(7).Info("resource created ", rpath)
	return &res, nil
}

func (ps *PGStorage) UpdateResource(rpath, content string) (*data.Resource, error) {
	logr := ps.log.WithValues("UpdateResource()", "PGStorage")
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
	res := data.NewResource(rpath, &PGResourceAdapter{db: ps.db, resourcePath: rpath, log: ps.log})
	logr.V(7).Info("resource updated ", rpath)
	return &res, nil
}

func (ps *PGStorage) DeleteResource(rpath string) error {
	logr := ps.log.WithValues("DeleteResource()", "PGStorage")
	_, err := ps.db.Exec("DELETE FROM calendar WHERE rpath = $1", rpath)
	if err != nil {
		logr.Info("failed to delete resource ", rpath, " ", err.Error())
		return err
	}
	return nil
}

func (ps *PGStorage) isResourcePresent(rpath string) bool {
	rows, err := ps.db.Query("SELECT rpath FROM calendar WHERE rpath = $1", rpath)
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
	db *sql.DB
	resourcePath string
	log logr.Logger
}

func (pa *PGResourceAdapter) CalculateEtag() string {
	if pa.IsCollection() {
		return ""
	}

	return fmt.Sprintf(`"%x%x"`, pa.GetContentSize(), pa.GetModTime().UnixNano())
}

func (pa *PGResourceAdapter) GetContent() string {
	logr := pa.log.WithValues("GetContent()")
	if pa.IsCollection() {
		return ""
	}
	rows, err := pa.db.Query("SELECT content FROM calendar WHERE rpath = $1", pa.resourcePath)
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
	if pa.resourcePath == "/" {
		return true
	}
	return false
}

func (pa *PGResourceAdapter) GetModTime() time.Time {
	logr := pa.log.WithValues("GetModTime()")
	rows, err := pa.db.Query("SELECT modified FROM calendar WHERE rpath = $1", pa.resourcePath)
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

func (s *server) myHandler(writer http.ResponseWriter, request *http.Request) {
	stg := new(PGStorage)
	stg.db = s.db
	stg.log = s.log
	caldav.SetupStorage(stg)
	// log.Printf("%v\n", request)
	// log.Printf("%v\n", request.Body)
	response := caldav.HandleRequest(request)
	// log.Printf("%v\n", response)
	// ... do something with the response object before writing it back to the client ...
	response.Write(writer)
}

func (s *server) setupDB() error {
	logr := s.log.WithValues("DB Setup")
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
	log := glogr.New().WithName("Julius")
	var confFlag string
	flag.StringVar(&confFlag, "conf", "julius.conf", "config file path")
	flag.Parse()

	conf := Defaults()

	if confFlag == "julius.conf" {
		_, err := os.Stat(confFlag)
		if err == nil {
			filename , err := filepath.Abs(confFlag)
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
	s := server{db: db, log: log}
	if err = s.setupDB() ; err != nil {
		os.Exit(1)
	}
	defer db.Close()
	http.HandleFunc("/", s.myHandler)
	http.ListenAndServe(conf.Host, nil)
}
