package store

// Base queries of the module. Placeholders are always "$1, $2, ...", which
// SQLite and Postgres both accept. The queries are static: no method builds
// clauses at run time.
//
// DELETE ... RETURNING and UPDATE ... RETURNING are supported by both
// databases, and save a read before the write.

const (
	accountColumns = `id, username, email, is_admin, disabled, created_at, updated_at`

	countAccounts = `SELECT count(*) FROM accounts`
	insertAccount = `
		INSERT INTO accounts (id, username, username_key, email, email_key, password_hash, is_admin, disabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	selectAccount        = `SELECT ` + accountColumns + ` FROM accounts WHERE id = $1`
	selectAccountByLogin = `SELECT ` + accountColumns + `, password_hash FROM accounts WHERE username_key = $1 OR email_key = $1`
	selectAccounts       = `SELECT ` + accountColumns + ` FROM accounts ORDER BY id`
)

const (
	profileColumns = `id, account_id, handle, display_name, created_at, updated_at`

	insertProfile = `INSERT INTO profiles (` + profileColumns + `) VALUES ($1, $2, $3, $4, $5, $6)`
	selectProfile = `SELECT ` + profileColumns + ` FROM profiles WHERE id = $1`
	// Profiles are ordered by ID, which is a UUIDv7: that is the creation
	// order.
	selectProfiles        = `SELECT ` + profileColumns + ` FROM profiles ORDER BY id`
	selectAccountProfiles = `SELECT ` + profileColumns + ` FROM profiles WHERE account_id = $1 ORDER BY id`
)

const (
	sessionColumns = `id, account_id, device_id, profile_id, created_at`

	// upsertDevice keeps the ID and the creation time of an existing
	// device, and returns them.
	upsertDevice = `
		INSERT INTO devices (id, account_id, identifier, name, client, version, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (account_id, identifier) DO UPDATE SET
			name = excluded.name,
			client = excluded.client,
			version = excluded.version,
			last_seen_at = excluded.last_seen_at
		RETURNING id, created_at`

	deleteDeviceSessions = `DELETE FROM sessions WHERE device_id = $1 RETURNING ` + sessionColumns
	insertSession        = `
		INSERT INTO sessions (id, token_hash, account_id, device_id, profile_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	selectSessionByToken = `SELECT ` + sessionColumns + ` FROM sessions WHERE token_hash = $1`

	// updateSessionProfile only binds profiles of the session account.
	updateSessionProfile = `
		UPDATE sessions SET profile_id = $1
		WHERE id = $2
			AND EXISTS (SELECT 1 FROM profiles p WHERE p.id = $1 AND p.account_id = sessions.account_id)
		RETURNING ` + sessionColumns
	deleteSession = `DELETE FROM sessions WHERE id = $1 RETURNING ` + sessionColumns
)
