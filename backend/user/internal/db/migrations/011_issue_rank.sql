-- Manual issue order (docs/issue-order-plan.md): a fractional rank per issue, ordered project-wide in byte order, and
-- whether anyone has ordered the project by hand (its default sort is then manual).
alter table projects add column if not exists manual_order boolean not null default false;
alter table issues add column if not exists rank text collate "C";
-- backfill, keeping today's tiebreak order (newest number first) as the initial manual order
update issues i set rank = lpad(o.rn::text, 10, '0') || 'V'
from (select id, row_number() over (partition by project_id order by number desc, id) rn from issues) o
where o.id = i.id and i.rank is null;
alter table issues alter column rank set not null;
-- deferrable, so a rebalance can renumber a whole project in one statement (set constraints … deferred)
do $$ begin
  alter table issues add constraint issues_project_id_rank_key unique (project_id, rank) deferrable initially immediate;
exception when duplicate_table or duplicate_object then null; end $$;
