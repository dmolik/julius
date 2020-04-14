CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS pgcrypto;
DROP TABLE IF EXISTS calendar;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS collection;
DROP TABLE IF EXISTS collection_role;
/*CREATE TABLE vcal (
	id uuid DEFAULT uuid_generate_v4(),
	created timestamp,
	modified timestamp,
	categories text,
	summary text,
	description text,
	dtstamp timestamp,
	dtstart timestamp,
	dtend timestamp,
	location text,
	organizer text,
	attendees text
);*/

CREATE TABLE calendar (
	id uuid DEFAULT uuid_generate_v4() PRIMARY KEY,
	owner_id INT, /* NOT NULL, */
	modified TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
	rpath   TEXT,
	content TEXT,
	collection uuid
);

CREATE TABLE collection (
	id uuid DEFAULT uuid_generate_v4() PRIMARY KEY,
	name TEXT,
	owner_id INT
);

DROP TYPE IF EXISTS perm;
CREATE TYPE perm AS ENUM ('admin', 'write', 'read', 'none');

CREATE TABLE collection_role (
	collection_id uuid,
	user_id INT,
	permission perm DEFAULT 'none'
);

/* INSERT INTO users (username, password) VALUES ("dan", crypt("somepassword", gen_salt('bf', 12))); */
CREATE TABLE users (
	id         SERIAL PRIMARY KEY,
	username   TEXT UNIQUE NOT NULL,
	email      TEXT,
	password   VARCHAR(64) NOT NULL, /* crypt('input', password) */
	firstname  TEXT,
	lastname   TEXT,
	isverified BOOLEAN DEFAULT false
);
CREATE INDEX user_index ON users (username, password);
CREATE INDEX user_cal_index ON users (id, firstname, lastname);
CREATE INDEX cal_index ON calendar (rpath);
CREATE INDEX cal_user_index ON calendar (rpath, owner_id);
