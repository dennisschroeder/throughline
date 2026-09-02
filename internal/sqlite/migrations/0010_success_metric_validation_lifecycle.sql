UPDATE context_records
SET status = 'untested'
WHERE kind = 'success_metric' AND status IN ('proposed', 'accepted');
