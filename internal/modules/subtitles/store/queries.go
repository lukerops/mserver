package store

// Base queries of the module. Placeholders are always "$1, $2, ...", which
// SQLite and Postgres both accept.

const (
	subtitleFileColumns = `file_id, location_id, path, dir, format`

	selectSubtitleFile  = `SELECT ` + subtitleFileColumns + ` FROM subtitle_files WHERE file_id = $1`
	selectSubtitleFiles = `SELECT ` + subtitleFileColumns + ` FROM subtitle_files WHERE location_id = $1 AND dir = $2 ORDER BY path, file_id`
	upsertSubtitleFile  = `
		INSERT INTO subtitle_files (file_id, location_id, path, dir, format)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (file_id) DO UPDATE SET
			location_id = excluded.location_id,
			path = excluded.path,
			dir = excluded.dir,
			format = excluded.format`
	deleteSubtitleFile = `DELETE FROM subtitle_files WHERE file_id = $1`
)

const (
	itemColumns = `item_id, item_type, file_id, location_id, path, dir`

	selectItem  = `SELECT ` + itemColumns + ` FROM media_items WHERE item_id = $1`
	selectItems = `SELECT ` + itemColumns + ` FROM media_items WHERE location_id = $1 AND dir = $2 ORDER BY path, item_id`
	upsertItem  = `
		INSERT INTO media_items (item_id, item_type, file_id, location_id, path, dir)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (item_id) DO UPDATE SET
			item_type = excluded.item_type,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			path = excluded.path,
			dir = excluded.dir`
	deleteItem = `DELETE FROM media_items WHERE item_id = $1`
)

const (
	matchColumns = `subtitle_id, item_id, item_type, file_id, location_id, dir, format,
		language, title, is_forced, is_hearing_impaired, source`

	selectMatches    = `SELECT ` + matchColumns + ` FROM matched_subtitles ORDER BY subtitle_id LIMIT $1 OFFSET $2`
	selectDirMatches = `SELECT ` + matchColumns + ` FROM matched_subtitles WHERE location_id = $1 AND dir = $2 ORDER BY subtitle_id`
	upsertMatch      = `
		INSERT INTO matched_subtitles (subtitle_id, item_id, item_type, file_id, location_id, dir, format,
			language, title, is_forced, is_hearing_impaired, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (subtitle_id) DO UPDATE SET
			item_id = excluded.item_id,
			item_type = excluded.item_type,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			dir = excluded.dir,
			format = excluded.format,
			language = excluded.language,
			title = excluded.title,
			is_forced = excluded.is_forced,
			is_hearing_impaired = excluded.is_hearing_impaired,
			source = excluded.source`
	deleteMatch = `DELETE FROM matched_subtitles WHERE subtitle_id = $1`
)
