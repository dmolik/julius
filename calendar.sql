CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
DROP TABLE IF EXISTS calendar;
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
	rpath text,
	content text,
	parent uuid,
	children uuid[],
	modified TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
