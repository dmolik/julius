CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS pgcrypto;
DROP TABLE IF EXISTS calendar;
DROP TABLE IF EXISTS users;
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
	id uuid DEFAULT uuid_generate_v4(),
	user_id INT NOT NULL,
	modified TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
	rpath   TEXT,
	content TEXT,
	parent uuid,
	children uuid[]
);

/* INSERT INTO users (username, password) VALUES ("dan", crypt("somepassword", gen_salt('bf'))); */
CREATE TABLE users (
	id         SERIAL PRIMARY KEY,
	username   TEXT NOT NULL,
	email      TEXT NOT NULL,
	password   VARCHAR(64) NOT NULL, /* crypt('input', password) */
	firstname  TEXT NOT NULL,
	lastname   TEXT NOT NULL,
	isverified BOOLEAN DEFAULT false
);

CREATE INDEX user_index ON users (username, password);
CREATE INDEX user_cal_index ON users (id, firstname, lastname);
CREATE INDEX cal_index ON calendar (rpath);
CREATE INDEX cal_user_index ON calendar (rpath, user_id);
