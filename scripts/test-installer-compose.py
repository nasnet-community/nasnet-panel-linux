#!/usr/bin/env python3
"""Check installation Compose variants without starting containers or using .env."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent.parent
# Public bcrypt fixture; this is not an installation credential.
HASH = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"


def main():
    with tempfile.TemporaryDirectory(prefix="nasnet-compose-test-") as directory:
        project = Path(directory)
        for name in (
            "docker-compose.yml",
            "docker-compose.sqlite.yml",
            "docker-compose.acme.yml",
        ):
            shutil.copyfile(ROOT / name, project / name)

        template = (ROOT / ".env.example").read_text()
        assert "ADMIN_PASSWORD_HASH=''" in template, "Example hash must be single-quoted"
        values = {
            "ADMIN_PASSWORD_HASH": "'" + HASH + "'",
            "JWT_SECRET_KEY": "test-only-" + "x" * 64,
            "APP_BASE_URL": "http://192.0.2.1:9761",
            "DB_PASSWORD": "test-only-database-password",
        }
        env = "\n".join(
            line.split("=", 1)[0] + "=" + values[line.split("=", 1)[0]]
            if line.split("=", 1)[0] in values else line
            for line in template.splitlines()
        ) + "\n"
        (project / ".env").write_text(env)
        config_keys = {
            line.split("=", 1)[0] for line in template.splitlines()
            if "=" in line and line.split("=", 1)[0].isidentifier()
        }
        process_env = {
            key: value for key, value in os.environ.items()
            if key not in config_keys and not key.startswith("COMPOSE_")
        }

        for sqlite, acme in ((False, False), (True, False), (False, True), (True, True)):
            command = [
                "docker", "compose", "--project-directory", str(project),
                "--env-file", str(project / ".env"),
                "-f", str(project / "docker-compose.yml"),
            ]
            if sqlite:
                command += ["-f", str(project / "docker-compose.sqlite.yml")]
            if acme:
                command += ["-f", str(project / "docker-compose.acme.yml")]
            command += ["config", "--format", "json"]
            result = subprocess.run(
                command, text=True, capture_output=True, check=True, env=process_env,
            )
            services = json.loads(result.stdout)["services"]
            app = services["app"]
            # Compose escapes literal dollars when rendering a reusable config.
            actual_hash = app["environment"]["ADMIN_PASSWORD_HASH"].replace("$$", "$")
            assert actual_hash == HASH, "Compose changed the quoted bcrypt hash"
            assert "variable is not set" not in result.stderr, result.stderr
            assert ("postgres" in services) == (not sqlite)
            assert ("postgres" in app.get("depends_on", {})) == (not sqlite)
            ports = {(str(port["published"]), port["target"]) for port in app["ports"]}
            assert ("9761", 9761) in ports
            assert (("80", 80) in ports) == acme, "ACME challenge port must be published"
            print(f"PASS: Compose sqlite={sqlite}, acme={acme}, bcrypt preserved")


if __name__ == "__main__":
    main()
