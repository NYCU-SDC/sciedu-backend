ALTER TABLE users
    ADD COLUMN grade_level INTEGER,
    ADD CONSTRAINT users_grade_level_check CHECK (grade_level BETWEEN 7 AND 12);
