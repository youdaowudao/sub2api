-- Pending automatic enable action for the existing scheduling switch.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS schedule_resume_at TIMESTAMPTZ;
