package store

// Base queries of the module. Placeholders are always "$1, $2, ...", which
// SQLite and Postgres both accept. Only the dynamic clauses (pagination) are
// appended by the methods.

const (
	insertLocation      = `INSERT INTO locations (id, driver, options, root, created_at) VALUES ($1, $2, $3, $4, $5)`
	selectLocationRoots = `SELECT id, root FROM locations`
	selectLocation      = `SELECT id, driver, options, created_at FROM locations WHERE id = $1`
	selectLocations     = `SELECT id, driver, options, created_at FROM locations ORDER BY id`
	deleteLocation      = `DELETE FROM locations WHERE id = $1`
)

const (
	selectFiles = `SELECT id, location_id, path, size, mod_time, inode FROM files`

	selectFile        = selectFiles + ` WHERE id = $1`
	selectFilesByPath = selectFiles + ` WHERE location_id = $1 ORDER BY path`
	selectSiblings    = `SELECT id, path FROM files WHERE location_id = $1 AND dir = $2 AND id <> $3 ORDER BY path`

	insertFile = `INSERT INTO files (id, location_id, path, dir, size, mod_time, inode)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	updateFile = `UPDATE files SET path = $1, dir = $2, size = $3, mod_time = $4, inode = $5 WHERE id = $6`
	deleteFile = `DELETE FROM files WHERE id = $1`
)

const (
	selectPendingAnalysis = `
		SELECT f.id, f.location_id, f.path, f.size, f.mod_time, f.inode
		FROM files f
		LEFT JOIN file_analyses a
			ON a.file_id = f.id AND a.analyzer = $1 AND a.version = $2
			AND a.size = f.size AND a.mod_time = f.mod_time
		WHERE a.file_id IS NULL AND f.mod_time <= $3
		ORDER BY f.id`

	upsertAnalysis = `
		INSERT INTO file_analyses (file_id, analyzer, version, size, mod_time, result, error, analyzed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (file_id, analyzer) DO UPDATE SET
			version = excluded.version,
			size = excluded.size,
			mod_time = excluded.mod_time,
			result = excluded.result,
			error = excluded.error,
			analyzed_at = excluded.analyzed_at`

	selectAnalyses = `
		SELECT a.file_id, a.analyzer, a.version, a.size, a.mod_time, a.result, a.error, a.analyzed_at
		FROM file_analyses a
		JOIN files f ON f.id = a.file_id AND f.size = a.size AND f.mod_time = a.mod_time
		WHERE f.location_id = $1 AND a.result IS NOT NULL
		ORDER BY a.file_id, a.analyzer`
)
