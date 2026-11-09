package outbox

const creationQuery = `
CREATE TABLE IF NOT EXISTS outbox (
	id      TEXT PRIMARY KEY, -- event ID (UUIDv7): orders the events
	topic   TEXT NOT NULL,
	payload BLOB NOT NULL
)
`

const insertQuery = `
INSERT INTO outbox (id, topic, payload) VALUES (?, ?, ?)
`

const getPendpings = `
SELECT id, topic, payload FROM outbox ORDER BY id LIMIT ?
`

const deleteJob = `
DELETE FROM outbox WHERE id = ?
`
