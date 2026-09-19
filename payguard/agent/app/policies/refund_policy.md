# Refund and Reversal Policy

When the payment provider reports a terminal state (succeeded, failed, or refunded) that
disagrees with our local record, the provider's state is always authoritative. Our local
system is a cache of the provider's truth, not the other way around. If the provider shows a
payment as refunded or failed while our local record still shows it succeeded, this is the
LOCAL_AHEAD case: the webhook that should have told us about the reversal never arrived, or
arrived and was lost before processing.

A LOCAL_AHEAD case is resolved by compensating the user: reversing whatever grant (currency,
items, tickets) was issued for that payment, then advancing the local payment record through
REFUND_PENDING to REFUNDED so it matches the provider. This is a safe default because the
provider has already told us, authoritatively, that the money came back to the user -- keeping
the grant in place after that would mean the user keeps both the refund and the goods.

Compensation is never permitted above the policy ceiling configured for automatic resolution
without a second human sign-off, regardless of how confident the evidence looks. A large
compensation amount is exactly the situation where a second pair of eyes matters more, not
less. The policy engine enforces this ceiling in code; no agent recommendation can override it.

If the provider's reported amount does not match our recorded amount for the same payment, do
not resolve automatically -- flag it for a human. An amount mismatch on a refund is a signal of
a bug or a partial refund, not something to bridge over silently.
