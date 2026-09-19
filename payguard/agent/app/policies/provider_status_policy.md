# Unknown Provider Status Policy

Payment providers occasionally introduce new status values, or our integration falls behind
a provider API change, and a status string arrives that our system does not recognize. This is
the UNKNOWN_PROVIDER_STATE case. Because the meaning of the status is, by definition, unknown to
us, no automatic action is ever appropriate here -- guessing whether an unrecognized status means
success or failure risks moving money in the wrong direction.

The correct response to UNKNOWN_PROVIDER_STATE is always NOTE_ONLY: record the raw status string
exactly as the provider sent it, and require a human to look it up in the provider's own
documentation or support channel before any resolution is applied. Once a human confirms what
the status means, the fix belongs in the code that maps provider statuses, not in a one-off
manual resolution -- otherwise the same unknown status will keep generating cases indefinitely.

STUCK_PROCESSING is a related but different case: the provider has no record of the payment at
all, or keeps timing out, past a reasonable threshold. If the provider explicitly has no record
(a 404-equivalent), the safe conclusion is usually that the payment attempt never went through
and can be marked failed. If the provider is merely timing out repeatedly, the state is genuinely
unknown and should default to NOTE_ONLY until connectivity or provider health is confirmed.
