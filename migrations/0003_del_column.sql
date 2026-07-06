CREATE TABLE users_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    age        INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO users_new (id, name, age, created_at) SELECT id, name, age, created_at FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;
