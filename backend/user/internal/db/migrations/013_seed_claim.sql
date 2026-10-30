-- which instance's editor seeds a live description from the saved one; a claim older than 10s is free to take
alter table issues add column seed_claimed_at timestamptz;
