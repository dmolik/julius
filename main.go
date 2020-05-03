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

	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path/filepath"

	_ "github.com/lib/pq"
	"github.com/dmolik/caldav-go"

	"github.com/dmolik/julius/mail"
	"github.com/dmolik/julius/storage"

)

type Config struct {
	db   string     `yaml:"db"`
	Host string     `yaml:"host"`
	Mail mail.Mail  `yaml:"smtp"`
}

func Defaults() *Config {
	return &Config{
		db:   "user=calendar database=calendar sslmode=disable",
		Host: "0.0.0.0:3000",
	}
}

type server struct {
	ctx context.Context
	db  *sql.DB
	log logr.Logger
	mail mail.Mail
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
	stg       := new(storage.PGStorage)
	stg.DB     = s.db
	stg.Log    = s.log
	stg.User   = username
	stg.UserID = id
	stg.Email  = email
	stg.Mailer = s.mail

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
	flag.StringVar(&confFlag, "conf", "julius.yaml", "config file path")
	flag.Parse()

	log := glogr.New().WithName("Julius")

	conf := Defaults()

	if confFlag == "julius.yaml" {
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

	db, err := sql.Open("postgres", conf.db)
	if err != nil {
		log.Error(err, "failed to open db")
		os.Exit(1)
	}
	s := server{db: db, log: log, mail: conf.Mail, ctx: context.Background()}
	if err = s.setupDB(); err != nil {
		os.Exit(1)
	}
	defer db.Close()
	log.V(2).Info("starting")
	http.HandleFunc("/", s.myHandler)
	http.ListenAndServe(conf.Host, nil)
}
