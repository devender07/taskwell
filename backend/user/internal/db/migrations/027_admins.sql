-- Platform admins get their own accounts, apart from app users: an admin is no longer a users row with a flag.
-- Existing admins carry over with the same id, so their second factor and sign-ins keep working.
create table admins (
    id uuid default uuidv7() primary key,
    name text not null,
    email text not null unique,
    password_hash text not null,
    created_at timestamptz default now() not null
);

insert into admins (id, name, email, password_hash)
select id, name, email, password_hash from users where platform_admin and password_hash is not null;

delete from admin_mfa where user_id not in (select id from admins);
delete from admin_sessions where user_id not in (select id from admins);

alter table admin_mfa drop constraint admin_mfa_user_id_fkey;
alter table admin_mfa rename column user_id to admin_id;
alter table admin_mfa add foreign key (admin_id) references admins (id) on delete cascade;

alter table admin_sessions drop constraint admin_sessions_user_id_fkey;
alter table admin_sessions rename column user_id to admin_id;
alter table admin_sessions add foreign key (admin_id) references admins (id) on delete cascade;

alter table users drop column platform_admin;
