-- Story points on issues, and each cycle's points per day for its burndown.
alter table issues_all add column estimate smallint check (estimate in (1, 2, 3, 5, 8));
-- issues is `select * from issues_all where deleted_at is null` (017); a view's * is fixed when it's created, so it's
-- recreated to show the new column. create or replace keeps the old columns in order and adds estimate at the end.
create or replace view issues as select * from issues_all where deleted_at is null;

-- A cycle's points at the end of each day, written by the snapshot job (cycle.Run). Ended cycles keep their last row.
create table cycle_points (
    cycle_id uuid not null references cycles (id) on delete cascade,
    day date not null,
    scope integer not null, -- estimated points in the cycle, canceled issues left out
    done integer not null,  -- of those, in a completed status
    primary key (cycle_id, day)
);
