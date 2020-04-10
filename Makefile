
default: build

build: bindata.go
	go build -v .

schema:
	psql -U calendar calendar -f calendar.sql

clean: clean-db

clean-db:
	psql -U calendar calendar -c 'DELETE FROM calendar;'

bindata.go: sql
	go-bindata sql
