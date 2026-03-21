#!/usr/bin/env python3
"""Sync a root .env file into Railway variables via the GraphQL API.

This keeps the repo-side env contract versioned while Railway remains the
runtime source of truth for deployment variables.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

RAILWAY_GRAPHQL_URL = "https://backboard.railway.com/graphql/v2"


def parse_env_file(path: Path) -> list[tuple[str, str]]:
    pairs: list[tuple[str, str]] = []
    for lineno, raw_line in enumerate(path.read_text().splitlines(), start=1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError(f"{path}:{lineno}: expected KEY=VALUE")
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if not key:
            raise ValueError(f"{path}:{lineno}: variable name cannot be empty")
        if value and value[0] == value[-1] and value[0] in {"'", '"'}:
            value = value[1:-1]
        pairs.append((key, value))
    if not pairs:
        raise ValueError(f"{path}: no variables found")
    return pairs


def load_bearer_token() -> str:
    for env_var in ("RAILWAY_TOKEN", "RAILWAY_API_TOKEN"):
        value = os.environ.get(env_var, "").strip()
        if value:
            return value
    raise ValueError("set RAILWAY_TOKEN or RAILWAY_API_TOKEN")


def resolve_shared_env_vars(names: list[str]) -> list[tuple[str, str]]:
    pairs: list[tuple[str, str]] = []
    for name in names:
        value = os.environ.get(name, "").strip()
        if not value:
            raise ValueError(f"set {name} before syncing shared Railway variables")
        pairs.append((name, value))
    return pairs


def graphql_request(token: str, query: str, variables: dict | None = None) -> dict:
    payload = {"query": query}
    if variables is not None:
        payload["variables"] = variables

    try:
        result = subprocess.run(
            [
                "curl",
                "-sS",
                RAILWAY_GRAPHQL_URL,
                "-H",
                "Content-Type: application/json",
                "-H",
                f"Authorization: Bearer {token}",
                "--data",
                json.dumps(payload),
            ],
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        detail = exc.stderr or exc.stdout
        raise RuntimeError(f"Railway API request failed: {detail}") from exc

    decoded = json.loads(result.stdout)
    if decoded.get("errors"):
        raise RuntimeError(json.dumps(decoded["errors"], indent=2))
    return decoded["data"]


def discover_project_context(
    token: str,
    service_name: str,
    environment_name: str,
    project_id: str | None,
    project_name: str | None,
) -> tuple[dict, dict, dict]:
    if project_id:
        data = graphql_request(
            token,
            """
            query($projectId: String!) {
              project(id: $projectId) {
                id
                name
                environments { edges { node { id name } } }
                services { edges { node { id name } } }
              }
            }
            """,
            {"projectId": project_id},
        )
        project = data["project"]
    else:
        data = graphql_request(
            token,
            """
            query {
              projects {
                edges {
                  node {
                    id
                    name
                    environments { edges { node { id name } } }
                    services { edges { node { id name } } }
                  }
                }
              }
            }
            """,
        )
        projects = [edge["node"] for edge in data["projects"]["edges"]]
        if project_name:
            projects = [project for project in projects if project["name"] == project_name]
        else:
            projects = [
                project
                for project in projects
                if any(
                    edge["node"]["name"] == service_name
                    for edge in project["services"]["edges"]
                )
                and any(
                    edge["node"]["name"] == environment_name
                    for edge in project["environments"]["edges"]
                )
            ]
        if len(projects) != 1:
            raise ValueError(
                "unable to select a unique Railway project; set --project-id or --project-name"
            )
        project = projects[0]

    services = [edge["node"] for edge in project["services"]["edges"]]
    service = next((item for item in services if item["name"] == service_name), None)
    if service is None:
        raise ValueError(
            f"service {service_name!r} not found in Railway project {project['name']!r}"
        )

    environments = [edge["node"] for edge in project["environments"]["edges"]]
    environment = next(
        (item for item in environments if item["name"] == environment_name),
        None,
    )
    if environment is None:
        raise ValueError(
            f"environment {environment_name!r} not found in Railway project {project['name']!r}"
        )

    return project, environment, service


def variable_map(pairs: list[tuple[str, str]]) -> dict[str, str]:
    return {key: value for key, value in pairs}


def mask_value(key: str, value: str) -> str:
    upper = key.upper()
    if any(marker in upper for marker in ("TOKEN", "SECRET", "PASSWORD", "KEY")):
        if len(value) <= 8:
            return "*" * len(value)
        return value[:4] + "..." + value[-4:]
    return value


def upsert_variable_collection(
    token: str,
    *,
    project_id: str,
    environment_id: str,
    variables: dict[str, str],
    skip_deploys: bool,
    service_id: str | None = None,
) -> bool:
    input_payload: dict[str, object] = {
        "projectId": project_id,
        "environmentId": environment_id,
        "variables": variables,
        "skipDeploys": skip_deploys,
    }
    if service_id is not None:
        input_payload["serviceId"] = service_id

    data = graphql_request(
        token,
        """
        mutation($input: VariableCollectionUpsertInput!) {
          variableCollectionUpsert(input: $input)
        }
        """,
        {"input": input_payload},
    )
    return bool(data["variableCollectionUpsert"])


def fetch_variables(
    token: str,
    *,
    project_id: str,
    environment_id: str,
    service_id: str | None = None,
    unrendered: bool = True,
) -> dict[str, str]:
    query = """
    query(
      $projectId: String!,
      $environmentId: String!,
      $serviceId: String,
      $unrendered: Boolean
    ) {
      variables(
        projectId: $projectId,
        environmentId: $environmentId,
        serviceId: $serviceId,
        unrendered: $unrendered
      )
    }
    """
    variables = {
        "projectId": project_id,
        "environmentId": environment_id,
        "serviceId": service_id,
        "unrendered": unrendered,
    }
    data = graphql_request(token, query, variables)
    return data["variables"]


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Sync a root .env file into Railway service variables."
    )
    parser.add_argument(
        "--env-file",
        default=".env.production.example",
        help="Root env file to import (default: .env.production.example)",
    )
    parser.add_argument(
        "--project-id",
        default=os.environ.get("RAILWAY_PROJECT_ID", "").strip() or None,
        help="Railway project id (default: $RAILWAY_PROJECT_ID or auto-discover)",
    )
    parser.add_argument(
        "--project-name",
        default=os.environ.get("RAILWAY_PROJECT_NAME", "").strip() or None,
        help="Railway project name (default: $RAILWAY_PROJECT_NAME or auto-discover)",
    )
    parser.add_argument(
        "--service",
        default=os.environ.get("RAILWAY_SERVICE", "wasteland"),
        help="Railway service name (default: $RAILWAY_SERVICE or wasteland)",
    )
    parser.add_argument(
        "--environment",
        default=os.environ.get("RAILWAY_ENVIRONMENT", "production"),
        help="Railway environment name (default: $RAILWAY_ENVIRONMENT or production)",
    )
    parser.add_argument(
        "--shared-env-var",
        action="append",
        default=[],
        help="Environment variable name to also upsert as a shared Railway variable",
    )
    parser.add_argument(
        "--no-skip-deploys",
        action="store_true",
        help="Apply variable changes without skipDeploys",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the Railway API actions without executing them",
    )
    args = parser.parse_args()

    env_path = Path(args.env_file)
    if not env_path.exists():
        print(f"env file not found: {env_path}", file=sys.stderr)
        return 1

    try:
        pairs = parse_env_file(env_path)
        shared_pairs = resolve_shared_env_vars(args.shared_env_var)
        token = load_bearer_token()
        project, environment, service = discover_project_context(
            token=token,
            service_name=args.service,
            environment_name=args.environment,
            project_id=args.project_id,
            project_name=args.project_name,
        )
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    service_variables = variable_map(pairs)
    shared_variables = variable_map(shared_pairs)
    skip_deploys = not args.no_skip_deploys

    if args.dry_run:
        print(
            json.dumps(
                {
                    "project": {"id": project["id"], "name": project["name"]},
                    "environment": {
                        "id": environment["id"],
                        "name": environment["name"],
                    },
                    "service": {"id": service["id"], "name": service["name"]},
                    "skip_deploys": skip_deploys,
                    "shared_variable_keys": list(shared_variables.keys()),
                    "service_variables": {
                        key: mask_value(key, value)
                        for key, value in service_variables.items()
                    },
                },
                indent=2,
                sort_keys=True,
            )
        )
        return 0

    try:
        if shared_variables:
            upsert_variable_collection(
                token,
                project_id=project["id"],
                environment_id=environment["id"],
                variables=shared_variables,
                skip_deploys=skip_deploys,
            )
        upsert_variable_collection(
            token,
            project_id=project["id"],
            environment_id=environment["id"],
            service_id=service["id"],
            variables=service_variables,
            skip_deploys=skip_deploys,
        )
        verified_service = fetch_variables(
            token,
            project_id=project["id"],
            environment_id=environment["id"],
            service_id=service["id"],
            unrendered=True,
        )
        verified_shared = fetch_variables(
            token,
            project_id=project["id"],
            environment_id=environment["id"],
            unrendered=True,
        )
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    missing_service_keys = sorted(set(service_variables) - set(verified_service))
    missing_shared_keys = sorted(set(shared_variables) - set(verified_shared))
    if missing_service_keys or missing_shared_keys:
        if missing_shared_keys:
            print(
                f"shared Railway variables missing after sync: {', '.join(missing_shared_keys)}",
                file=sys.stderr,
            )
        if missing_service_keys:
            print(
                f"service Railway variables missing after sync: {', '.join(missing_service_keys)}",
                file=sys.stderr,
            )
        return 1

    print(
        json.dumps(
            {
                "project": project["name"],
                "environment": environment["name"],
                "service": service["name"],
                "shared_variable_keys": sorted(shared_variables.keys()),
                "service_variable_keys": sorted(service_variables.keys()),
                "skip_deploys": skip_deploys,
            },
            indent=2,
            sort_keys=True,
        )
    )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
