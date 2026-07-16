ALTER TABLE qa_record
ADD COLUMN retrieval_trace JSONB NOT NULL DEFAULT '{}'::jsonb;
