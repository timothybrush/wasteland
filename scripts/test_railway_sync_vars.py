import os
import tempfile
import unittest
from pathlib import Path

from railway_sync_vars import (
    discover_project_context,
    mask_value,
    parse_env_file,
    resolve_shared_env_vars,
    variable_map,
)


class RailwaySyncVarsTest(unittest.TestCase):
    def test_parse_env_file_preserves_railway_references(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            env_file = Path(tmp_dir) / ".env.production.example"
            env_file.write_text(
                "\n".join(
                    [
                        "# comment",
                        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://otel.cloud.gascityhall.com/v1/traces",
                        "OTEL_EXPORTER_OTLP_HEADERS=X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}",
                        'WL_BROWSER_OTLP_HEADERS="X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}"',
                    ]
                )
            )

            pairs = parse_env_file(env_file)

        self.assertEqual(
            pairs,
            [
                (
                    "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
                    "https://otel.cloud.gascityhall.com/v1/traces",
                ),
                (
                    "OTEL_EXPORTER_OTLP_HEADERS",
                    "X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}",
                ),
                (
                    "WL_BROWSER_OTLP_HEADERS",
                    "X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}",
                ),
            ],
        )

    def test_resolve_shared_env_vars_uses_process_env(self) -> None:
        original = os.environ.get("OTLP_SHARED_TOKEN")
        self.addCleanup(self._restore_env, "OTLP_SHARED_TOKEN", original)
        os.environ["OTLP_SHARED_TOKEN"] = "token-value"
        self.assertEqual(
            resolve_shared_env_vars(["OTLP_SHARED_TOKEN"]),
            [("OTLP_SHARED_TOKEN", "token-value")],
        )

    def test_variable_map_keeps_string_values(self) -> None:
        self.assertEqual(
            variable_map(
                [
                    ("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://otel.cloud.gascityhall.com/v1/traces"),
                    ("OTEL_EXPORTER_OTLP_HEADERS", "X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}"),
                ]
            ),
            {
                "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://otel.cloud.gascityhall.com/v1/traces",
                "OTEL_EXPORTER_OTLP_HEADERS": "X-OTLP-Shared-Token=${{shared.OTLP_SHARED_TOKEN}}",
            },
        )

    def test_mask_value_masks_secret_like_values(self) -> None:
        self.assertEqual(mask_value("OTLP_SHARED_TOKEN", "abc123456789"), "abc1...6789")
        self.assertEqual(
            mask_value(
                "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
                "https://otel.cloud.gascityhall.com/v1/traces",
            ),
            "https://otel.cloud.gascityhall.com/v1/traces",
        )

    def test_discover_project_context_selects_matching_project_service_and_env(self) -> None:
        original_request = __import__("railway_sync_vars").graphql_request
        self.addCleanup(self._restore_graphql_request, original_request)

        def fake_graphql_request(token: str, query: str, variables=None):
            del token, query, variables
            return {
                "projects": {
                    "edges": [
                        {
                            "node": {
                                "id": "project-1",
                                "name": "comfortable-gentleness",
                                "environments": {
                                    "edges": [{"node": {"id": "env-1", "name": "production"}}]
                                },
                                "services": {
                                    "edges": [{"node": {"id": "svc-1", "name": "wasteland"}}]
                                },
                            }
                        }
                    ]
                }
            }

        import railway_sync_vars

        railway_sync_vars.graphql_request = fake_graphql_request

        project, environment, service = discover_project_context(
            token="token",
            service_name="wasteland",
            environment_name="production",
            project_id=None,
            project_name=None,
        )

        self.assertEqual(project["id"], "project-1")
        self.assertEqual(environment["id"], "env-1")
        self.assertEqual(service["id"], "svc-1")

    @staticmethod
    def _restore_env(name: str, value: str | None) -> None:
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value

    @staticmethod
    def _restore_graphql_request(original) -> None:
        import railway_sync_vars

        railway_sync_vars.graphql_request = original


if __name__ == "__main__":
    unittest.main()
