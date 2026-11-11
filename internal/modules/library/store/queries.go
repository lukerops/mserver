package store

// Base queries of the module. Placeholders are always "$1, $2, ...", which
// SQLite and Postgres both accept. Only the dynamic clauses (filters,
// order and pagination) are appended by the methods.
//
// The queries alias the tables: sr (series), sn (seasons) and ep
// (episodes).

const (
	insertLibrary         = `INSERT INTO libraries (id, name, media_type, created_at) VALUES ($1, $2, $3, $4)`
	insertLibraryLocation = `INSERT INTO library_locations (library_id, location_id, position) VALUES ($1, $2, $3)`
	selectLocationOwner   = `SELECT library_id FROM library_locations WHERE location_id = $1`
	deleteLibrary         = `DELETE FROM libraries WHERE id = $1`

	selectLibraries   = `SELECT id, name, media_type, created_at FROM libraries`
	selectLibrary     = selectLibraries + ` WHERE id = $1`
	selectLibraryOf   = selectLibraries + ` WHERE id = (SELECT library_id FROM library_locations WHERE location_id = $1)`
	orderLibraries    = ` ORDER BY lower(name), id`
	selectAllLocation = `SELECT library_id, location_id FROM library_locations ORDER BY library_id, position`
)

const (
	movieColumns = `id, library_id, file_id, location_id, path, title, sort_title, year, media_info, created_at, updated_at`

	selectMovie  = `SELECT ` + movieColumns + ` FROM movies WHERE id = $1`
	selectMovies = `SELECT ` + movieColumns + ` FROM movies`
	countMovies  = `SELECT COUNT(*) FROM movies`

	upsertMovie = `
		INSERT INTO movies (id, library_id, file_id, location_id, path, title, sort_title, year,
			media_info, audio_languages, subtitle_languages, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			library_id = excluded.library_id,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			path = excluded.path,
			title = excluded.title,
			sort_title = excluded.sort_title,
			year = excluded.year,
			media_info = excluded.media_info,
			audio_languages = excluded.audio_languages,
			subtitle_languages = excluded.subtitle_languages,
			updated_at = excluded.updated_at`
	updateMovieMediaInfo = `
		UPDATE movies SET media_info = $1, audio_languages = $2, subtitle_languages = $3, updated_at = $4
		WHERE id = $5`
	deleteMovie = `DELETE FROM movies WHERE id = $1`
)

const (
	selectSeriesBase = `SELECT sr.id, sr.library_id, sr.title, sr.sort_title, sr.key, sr.year, sr.created_at,
		(SELECT COUNT(*) FROM seasons WHERE series_id = sr.id),
		(SELECT COUNT(*) FROM episodes WHERE series_id = sr.id)
		FROM series AS sr`
	selectSeriesByID = selectSeriesBase + ` WHERE sr.id = $1`
	countSeries      = `SELECT COUNT(*) FROM series AS sr`

	findSeries   = `SELECT id, title FROM series WHERE library_id = $1 AND key = $2 AND year = $3`
	insertSeries = `INSERT INTO series (id, library_id, title, sort_title, key, year, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	selectSeriesInfo = `SELECT library_id, title, year FROM series WHERE id = $1`
	countSeriesEps   = `SELECT COUNT(*) FROM episodes WHERE series_id = $1`
	deleteSeries     = `DELETE FROM series WHERE id = $1`
)

const (
	selectSeasonBase = `SELECT sn.id, sn.series_id, sn.library_id, sn.number, sn.created_at, sr.title, sr.year,
		(SELECT COUNT(*) FROM episodes WHERE season_id = sn.id)
		FROM seasons AS sn JOIN series AS sr ON sr.id = sn.series_id`
	selectSeasonByID = selectSeasonBase + ` WHERE sn.id = $1`
	orderSeasons     = ` ORDER BY sr.sort_title, sr.id, sn.number`

	findSeason       = `SELECT id FROM seasons WHERE series_id = $1 AND number = $2`
	insertSeason     = `INSERT INTO seasons (id, series_id, library_id, number, created_at) VALUES ($1, $2, $3, $4, $5)`
	selectSeasonInfo = `SELECT series_id, library_id, number FROM seasons WHERE id = $1`
	countSeasonEps   = `SELECT COUNT(*) FROM episodes WHERE season_id = $1`
	deleteSeason     = `DELETE FROM seasons WHERE id = $1`
)

const (
	selectEpisodeBase = `SELECT ep.id, ep.library_id, ep.series_id, ep.season_id, ep.file_id, ep.location_id, ep.path,
		ep.title, ep.sort_key, ep.number, ep.end_number, ep.media_info, ep.created_at, ep.updated_at,
		sr.title, sr.year, sn.number
		FROM episodes AS ep
		JOIN series AS sr ON sr.id = ep.series_id
		JOIN seasons AS sn ON sn.id = ep.season_id`
	selectEpisodeByID = selectEpisodeBase + ` WHERE ep.id = $1`
	countEpisodes     = `SELECT COUNT(*) FROM episodes AS ep`

	selectEpisodeParents = `SELECT series_id, season_id FROM episodes WHERE id = $1`
	upsertEpisode        = `
		INSERT INTO episodes (id, library_id, series_id, season_id, file_id, location_id, path,
			title, sort_key, number, end_number, media_info, audio_languages, subtitle_languages,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (id) DO UPDATE SET
			library_id = excluded.library_id,
			series_id = excluded.series_id,
			season_id = excluded.season_id,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			path = excluded.path,
			title = excluded.title,
			sort_key = excluded.sort_key,
			number = excluded.number,
			end_number = excluded.end_number,
			media_info = excluded.media_info,
			audio_languages = excluded.audio_languages,
			subtitle_languages = excluded.subtitle_languages,
			updated_at = excluded.updated_at`
	updateEpisodeMediaInfo = `
		UPDATE episodes SET media_info = $1, audio_languages = $2, subtitle_languages = $3, updated_at = $4
		WHERE id = $5`
	deleteEpisode = `DELETE FROM episodes WHERE id = $1`
)
