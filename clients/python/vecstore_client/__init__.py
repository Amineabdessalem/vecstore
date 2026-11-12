"""Python client for the vecstore HTTP API (standard library only)."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any

__all__ = ["APIError", "Client", "Hit", "NotFound"]


class APIError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(f"vecstore: {status}: {message}")
        self.status = status
        self.message = message


class NotFound(APIError):
    pass


@dataclass(frozen=True)
class Hit:
    id: str
    distance: float
    metadata: dict[str, Any] = field(default_factory=dict)


class Client:
    """Talks to one vecstore server, e.g. ``Client("http://localhost:8080")``."""

    def __init__(self, base_url: str, api_key: str | None = None, timeout: float = 10.0):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def _request(self, method: str, path: str, body: Any = None) -> Any:
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base_url + path, data=data, method=method)
        if data is not None:
            req.add_header("Content-Type", "application/json")
        if self.api_key:
            req.add_header("Authorization", f"Bearer {self.api_key}")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            try:
                message = json.loads(e.read() or b"{}").get("error", e.reason)
            except ValueError:
                message = str(e.reason)
            raise (NotFound if e.code == 404 else APIError)(e.code, message) from None
        return json.loads(raw) if raw else None

    @staticmethod
    def _col(name: str) -> str:
        return "/collections/" + urllib.parse.quote(name, safe="")

    def create_collection(self, name: str, dim: int, metric: str = "cosine") -> dict[str, Any]:
        return self._request("POST", "/collections", {"name": name, "dim": dim, "metric": metric})

    def collections(self) -> list[dict[str, Any]]:
        return self._request("GET", "/collections")["collections"]

    def collection(self, name: str) -> dict[str, Any]:
        return self._request("GET", self._col(name))

    def drop_collection(self, name: str) -> None:
        self._request("DELETE", self._col(name))

    def upsert(self, name: str, records: list[dict[str, Any]]) -> int:
        """Inserts or replaces ``{"id", "values", "metadata"?}`` records; returns the collection size."""
        return self._request("PUT", self._col(name) + "/vectors", {"vectors": records})["count"]

    def get(self, name: str, id: str) -> dict[str, Any]:
        return self._request("GET", f"{self._col(name)}/vectors/{urllib.parse.quote(id, safe='')}")

    def delete(self, name: str, id: str) -> None:
        self._request("DELETE", f"{self._col(name)}/vectors/{urllib.parse.quote(id, safe='')}")

    def search(
        self, name: str, vector: list[float], k: int = 10, ef: int | None = None,
        filter: dict[str, Any] | None = None,
    ) -> list[Hit]:
        body: dict[str, Any] = {"vector": vector, "k": k}
        if ef:
            body["ef"] = ef
        if filter:
            body["filter"] = filter
        hits = self._request("POST", self._col(name) + "/search", body)["hits"]
        return [Hit(h["id"], h["distance"], h.get("metadata") or {}) for h in hits]

    def compact(self, name: str) -> int:
        return self._request("POST", self._col(name) + "/compact")["removed"]
