package store

func (s *Store) migrate() error {
	_, err := s.db.Exec(`PRAGMA journal_mode=WAL;
 PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY,task_id TEXT NOT NULL,started INTEGER NOT NULL,state TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS runs_started ON runs(started DESC);
 CREATE INDEX IF NOT EXISTS runs_task ON runs(task_id,started DESC);
 CREATE TABLE IF NOT EXISTS admin(id INTEGER PRIMARY KEY CHECK(id=1),username TEXT NOT NULL,password_hash TEXT NOT NULL);
 PRAGMA user_version=1;`)
	return err
}
