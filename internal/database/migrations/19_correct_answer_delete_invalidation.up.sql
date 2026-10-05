-- A deleted CorrectAnswer may be recreated at version 1. Clear its derived
-- results in the deleting transaction, including deletes caused by option FKs.
CREATE FUNCTION invalidate_deleted_correct_answer_results() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE answer_results AS ar
    SET status = 'PENDING', is_correct = NULL, graded_at = NULL,
        correct_answer_version = NULL
    FROM answers AS a
    WHERE ar.answer_id = a.id AND a.question_id = OLD.question_id
      AND ar.method = 'DETERMINISTIC';
    RETURN OLD;
END;
$$;

CREATE TRIGGER correct_answer_delete_invalidation
AFTER DELETE ON correct_answers
FOR EACH ROW EXECUTE FUNCTION invalidate_deleted_correct_answer_results();
