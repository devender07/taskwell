-- Fast search: plain description text alongside the rich-text doc, and trigram indexes for ilike and word_similarity.
create extension if not exists pg_trgm;
alter table issues add column description_text text not null default '';
update issues set description_text = coalesce((select string_agg(t #>> '{}', ' ') from jsonb_path_query(description, 'strict $.**.text') t), '') where description is not null;
create index on issues using gin (title gin_trgm_ops);
create index on issues using gin (description_text gin_trgm_ops);
create index on projects using gin (name gin_trgm_ops);
