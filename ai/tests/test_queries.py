import pytest

from ai_service.repositories.queries import _parse_queries


def test_named_sql_sections_preserve_query_text_and_parameters():
    first = "\n    SELECT %(query)s, %(role)s;\n"
    second = "SELECT context FROM contexts WHERE key = ?\n"
    assert _parse_queries(f"-- name: search\n{first}-- name: cache\n{second}") == {
        "search": first, "cache": second,
    }


@pytest.mark.parametrize("text", [
    "SELECT 1;\n",
    "SELECT 1;\n-- name: query\nSELECT 2;\n",
    "-- name: query\nSELECT 1;\n-- name: query\nSELECT 2;\n",
    "-- name: query\n \n",
])
def test_malformed_sql_resources_fail_before_serving_requests(text):
    with pytest.raises(ValueError):
        _parse_queries(text)
