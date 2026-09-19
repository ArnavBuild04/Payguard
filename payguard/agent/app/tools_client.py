"""The agent's only way of seeing the world: read-only HTTP calls to the Go core's
/internal/tools/* endpoints. There is no method on this class that can write anything --
that boundary is enforced by the Go core (those routes are all GET), not just by convention
here, but this client mirrors it so nothing is ever tempted to add a write call beside it.
"""

import httpx

from app.config import settings


class CoreToolsClient:
    def __init__(self, base_url: str | None = None, timeout: float = 5.0):
        self._client = httpx.Client(base_url=base_url or settings.core_base_url, timeout=timeout)

    def get_case(self, case_id: int) -> dict | None:
        r = self._client.get(f"/internal/tools/cases/{case_id}")
        if r.status_code == 404:
            return None
        r.raise_for_status()
        return r.json()

    def get_payment(self, payment_id: int) -> dict | None:
        r = self._client.get(f"/internal/tools/payments/{payment_id}")
        if r.status_code == 404:
            return None
        r.raise_for_status()
        return r.json()

    def get_wallet_balance(self, tenant_id: str, user_id: int) -> dict | None:
        r = self._client.get(f"/internal/tools/wallet/{tenant_id}/{user_id}/balance")
        if r.status_code == 404:
            return None
        r.raise_for_status()
        return r.json()

    def get_provider_payment(self, provider_payment_id: str) -> dict | None:
        if not provider_payment_id:
            return None
        r = self._client.get(f"/internal/tools/provider/payments/{provider_payment_id}")
        if r.status_code in (404, 502):
            return None
        r.raise_for_status()
        return r.json()

    def close(self) -> None:
        self._client.close()
