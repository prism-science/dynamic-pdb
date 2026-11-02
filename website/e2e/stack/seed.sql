-- Test users for the browser tests. The website Makefile passes the ids from
-- stack.env as psql variables. Running it again changes nothing.

insert into users (id, source, external_ref, email, display_name, avatar_url, created_at, updated_at)
values
    (:'submitter_id', 'browser-test', 'submitter', null, 'Browser Test Submitter', null, now(), now()),
    (:'reviewer_id', 'browser-test', 'reviewer', null, 'Browser Test Reviewer', null, now(), now())
on conflict (id) do nothing;

-- Only the reviewer may approve revisions. The submitter has no role, like a new depositor.
insert into user_roles (user_id, role_id)
select :'reviewer_id', roles.id
from roles
where roles.key = 'reviewer'
on conflict (user_id, role_id) do nothing;
