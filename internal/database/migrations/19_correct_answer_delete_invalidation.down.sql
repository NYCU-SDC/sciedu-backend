DROP TRIGGER IF EXISTS correct_answer_delete_invalidation ON correct_answers;
DROP FUNCTION IF EXISTS invalidate_deleted_correct_answer_results();
