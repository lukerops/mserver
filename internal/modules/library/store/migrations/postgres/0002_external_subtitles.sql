-- Subtitle files found beside the video, matched by the subtitles module.
-- JSON list; NULL when there are none.
ALTER TABLE movies ADD COLUMN external_subtitles TEXT;
ALTER TABLE episodes ADD COLUMN external_subtitles TEXT;
