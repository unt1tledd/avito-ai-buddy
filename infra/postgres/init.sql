\set ON_ERROR_STOP on

\getenv ai_password AI_DB_PASSWORD
\getenv user_password USER_DB_PASSWORD
\getenv auth_password AUTH_DB_PASSWORD

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', role_name, role_password)
FROM (
    VALUES
        ('ai_app', :'ai_password'),
        ('users_app', :'user_password'),
        ('auth_app', :'auth_password')
) AS requested(role_name, role_password)
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname::text = requested.role_name
)
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', database_name, owner_name)
FROM (
    VALUES
        ('users', 'users_app'),
        ('auth_db', 'auth_app')
) AS requested(database_name, owner_name)
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_database
    WHERE datname::text = requested.database_name
)
\gexec

\connect ai_buddy

CREATE EXTENSION IF NOT EXISTS vector;
ALTER DATABASE ai_buddy OWNER TO ai_app;