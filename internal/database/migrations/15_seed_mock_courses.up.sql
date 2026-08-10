INSERT INTO courses (id, code, title, description, status)
VALUES
    (
        '00000000-0000-0000-0000-000000000012',
        'MOCK-COURSE-ASSIGNED',
        'Mock Assigned Course',
        'Temporary development Course assigned to the deterministic mock Student.',
        'PUBLISHED'
    ),
    (
        '00000000-0000-0000-0000-000000000013',
        'MOCK-COURSE-UNASSIGNED',
        'Mock Unassigned Course',
        'Temporary development Course not assigned to the deterministic mock Student.',
        'PUBLISHED'
    )
ON CONFLICT (id) DO NOTHING;
