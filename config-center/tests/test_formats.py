import pytest

from server.config_io.formats import validate_content


def test_dotenv_content_is_kept_verbatim() -> None:
    assert validate_content("# comment\nTOKEN=a=b # untouched\n", "env") is None


def test_yaml_syntax_is_validated() -> None:
    assert validate_content("server:\n  port: 8001\n", "yaml") == {"server": {"port": 8001}}
    with pytest.raises(Exception):
        validate_content("server: [\n", "yaml")


def test_json_syntax_is_validated() -> None:
    assert validate_content('{"enabled": true}', "json") == {"enabled": True}
    with pytest.raises(ValueError):
        validate_content('{"enabled":', "json")
