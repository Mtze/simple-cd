-- The UI shows pull request artifacts with their title.
ALTER TABLE artifacts ADD COLUMN pr_title text NOT NULL DEFAULT '';
