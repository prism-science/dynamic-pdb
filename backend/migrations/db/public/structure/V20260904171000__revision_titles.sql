ALTER TABLE entry_revisions DROP COLUMN name;
ALTER TABLE entry_revisions RENAME COLUMN description TO title;

ALTER TABLE model_revisions DROP COLUMN description;
ALTER TABLE model_revisions RENAME COLUMN name TO title;
