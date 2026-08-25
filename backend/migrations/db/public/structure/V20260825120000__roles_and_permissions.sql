CREATE TABLE roles (
    id         uuid PRIMARY KEY,
    key        text NOT NULL UNIQUE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id          uuid PRIMARY KEY,
    key         text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_id       uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,

    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_by uuid REFERENCES users(id) ON DELETE SET NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_id_idx
    ON user_roles(role_id);

INSERT INTO roles (id, key, name)
VALUES
    ('6d8a2344-ee46-4ae0-b9a9-1f49f9ab84e1', 'admin', 'Administrator'),
    ('595eaceb-d35d-4839-aa92-3d15577776d8', 'reviewer', 'Reviewer');

INSERT INTO permissions (id, key, name, description)
VALUES
    ('566262a5-617a-4a86-aca9-910f7f135fe2', 'revisions.approve', 'Approve revisions', 'Can approve revisions'),
    ('0454bf9b-96de-456a-88fb-0b857021fc7e', 'revisions.reject', 'Reject revisions', 'Can reject revisions'),
    ('e152f71c-3e50-4cca-bd50-749fd0b86a81', 'roles.assign', 'Assign roles', 'Can assign roles to users'),
    ('c1ebac40-0b88-4510-b358-8124d15bbc5e', 'roles.revoke', 'Revoke roles', 'Can revoke roles from users');

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM (
    VALUES
        ('admin', 'roles.assign'),
        ('admin', 'roles.revoke'),
        ('reviewer', 'revisions.approve'),
        ('reviewer', 'revisions.reject')
) AS mapping(role_key, permission_key)
JOIN roles ON roles.key = mapping.role_key
JOIN permissions ON permissions.key = mapping.permission_key;
