-- Fixed IDs keep the demo options stable across restarts.
INSERT INTO users.teams (team_id, name)
VALUES
    (900001, 'Backend'),
    (900002, 'Frontend'),
    (900003, 'Data Analytics'),
    (900004, 'Platform'),
    (900005, 'QA')
ON CONFLICT (team_id) DO NOTHING;

INSERT INTO users.profiles (user_id, last_name, first_name)
VALUES
    (910001, 'Орлова', 'Анна'),
    (910002, 'Волков', 'Илья'),
    (910003, 'Белова', 'Мария'),
    (910004, 'Соколов', 'Артём'),
    (910005, 'Кузнецова', 'Елена')
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO users.team_members (team_id, user_id, role)
VALUES
    (900001, 910001, 'lead'),
    (900002, 910002, 'lead'),
    (900003, 910003, 'lead'),
    (900004, 910004, 'lead'),
    (900005, 910005, 'lead')
ON CONFLICT (team_id, user_id) DO NOTHING;
