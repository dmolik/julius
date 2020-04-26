# Julius

A CalDav calendaring server implemented using [Go](https://golang.org), [github.com/samedi/caldav-go](https://github.com/samedi/caldav-go), and [Postgres](https://www.postgresql.org/) designed to be small and efficient.

## Building

To build Julius you will need Golang and a Make variant. Then, Simply run `make`.

## Running

Setup your database:

    CREATE DATABASE calendar;
    CREATE USER calendar WITH PASSWORD '$PASSWORD';
    GRANT ALL  PRIVILEGES ON DATABASE "calendar" to calendar;
    \c calendar
    CREATE EXTENSION IF NOT EXISTS pgcrypto;
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";


Then just execute the Julius binary `./julius -conf julius.conf`
