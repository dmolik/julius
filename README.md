# Julius

An incomplete calendar app using [github.com/samedi/caldav-go](https://github.com/samedi/caldav-go) and [Postgres](https://www.postgresql.org/) designed to be small an efficient.

## Building

To build Julius you will need Golang and Make and Simply run `make`

## Setup

At the moment Julius doesn't set up it's own database so you will need to run `make schema`

## Running

Setup your database:

    CREATE DATABASE calendar;
    CREATE USER calendar WITH PASSWORD '$PASSWORD';
    GRANT ALL  PRIVILEGES ON DATABASE "calendar" to calendar;
    \c calendar
    CREATE EXTENSION IF NOT EXISTS pgcrypto;
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";


Then just execute the Julius binary `./julius -conf julius.conf`
