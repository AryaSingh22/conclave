// Package match holds the off-chain reference solver's shared, dependency-light
// primitives: the decoded OPEN-intent shape and the asset-key / numeraire
// helpers the cycle and pairwise matchers build on.
//
// This package imports ONLY proto message types (x/veil/types,
// x/session/types, x/escrow/types) — never x/session/keeper. The reference solver is an
// external, permissionless gRPC client (SPEC §6.4, §7): it observes open
// sessions and proposes matches, but the chain's on-chain 6-rule validation is
// the sole authority over whether a match is accepted. The matching IDEA is
// therefore reimplemented off-chain here (a separate implementation from the
// in-consensus fallback, per the build plan); the one rule that MUST NOT drift
// — price-binding representability (D-047/D-055) — is not reimplemented at all
// but reused verbatim from the chain via solver/gate (D-054).
package match

import (
	"encoding/hex"
	"strings"

	"cosmossdk.io/math"

	escrowtypes "github.com/AryaSingh22/conclave/x/escrow/types"
	sessiontypes "github.com/AryaSingh22/conclave/x/session/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// OpenIntent is an OPEN SealedIntent decoded into the give/want legs a matcher
// reasons over. It mirrors the on-chain view a solver gets from the veil
// Intents(state=OPEN) query: the payload give/want legs and the envelope
// allow_partial flag.
type OpenIntent struct {
	ID           string
	Sigil        string
	Give         []veiltypes.IntentLeg
	Want         []veiltypes.IntentLeg
	AllowPartial bool
	// Condition is the PAYLOAD-LEVEL delivery condition this intent signed
	// (IntentPayload.condition), governing every data/service leg the intent
	// carries unless a leg refines it with its own IntentLeg.condition (which
	// travels inside Give/Want). Nil means the intent signed NONE.
	//
	// Carried since task 2.14 (D-097) because it is now REQUIRED, not optional:
	// task 2.7 made a signed condition a requirement rather than a permission
	// ceiling, so a solver that cannot see this field proposes a solution that
	// DROPS it and is slashed 5% (ErrConditionNotHonored, session 1429). Before
	// 2.14 DecodeOpenIntent discarded it, which is precisely why the in-tree
	// reference solver carried that live defect.
	Condition *escrowtypes.DeliveryCondition
}

// Payload reassembles the signed IntentPayload fields the SPEC §6.5 delivery-
// condition binding reads, so an OpenIntent can be handed to the chain's own
// resolver and consent predicate (keeper.EffectiveSellerCondition,
// keeper.CheckConditionBinding) instead of a mirrored copy of them.
//
// It carries give, want, and the payload-level condition — exactly the fields
// that binding consults. counterparty_class is deliberately absent: it is §6.4
// rule 5, evaluated by the chain against Sigil state a solver does not hold.
func (oi OpenIntent) Payload() veiltypes.IntentPayload {
	return veiltypes.IntentPayload{Give: oi.Give, Want: oi.Want, Condition: oi.Condition}
}

// DecodeOpenIntent reduces a veil IntentRecord to an OpenIntent by unmarshaling
// its (Phase-0: plaintext) ciphertext as an IntentPayload and its envelope for
// the allow_partial flag. A record whose payload does not unmarshal is not a
// matchable intent (on-chain it would be INVALID at the Reveal); ok is false.
func DecodeOpenIntent(rec veiltypes.IntentRecord) (OpenIntent, bool) {
	var payload veiltypes.IntentPayload
	if err := payload.Unmarshal(rec.Intent.Ciphertext); err != nil {
		return OpenIntent{}, false
	}
	var env veiltypes.IntentEnvelope
	_ = env.Unmarshal(rec.Intent.Envelope) // best-effort; default allow_partial=false
	return OpenIntent{
		ID:           rec.Intent.Id,
		Sigil:        rec.Intent.SenderSigil,
		Give:         payload.Give,
		Want:         payload.Want,
		AllowPartial: env.AllowPartial,
		Condition:    payload.Condition,
	}, true
}

// SingleGiveWant reports the intent's single give and want legs, or ok=false if
// it does not have exactly one of each. Phase-0 intents express one give and
// one want (richer shapes are left to future solver iterations), exactly as the
// in-consensus fallback handles them.
func (oi OpenIntent) SingleGiveWant() (give, want veiltypes.IntentLeg, ok bool) {
	if len(oi.Give) != 1 || len(oi.Want) != 1 {
		return veiltypes.IntentLeg{}, veiltypes.IntentLeg{}, false
	}
	return oi.Give[0], oi.Want[0], true
}

// LegKeyAmount returns the conservation/pricing asset key and Phase-0 quantity
// of a give/want leg: coins by denom, service vouchers as svc/{schema}, data
// grants as data/{hex(content_hash)} with the bookkeeping quantity 1. This is a
// faithful off-chain reimplementation of the chain's intentLegKeyAmount /
// legAssetKey (the keys must be byte-identical, since price-binding values flows
// keyed on exactly these strings — D-006, conservation.go).
func LegKeyAmount(leg veiltypes.IntentLeg) (key string, amount math.Int, ok bool) {
	set := 0
	amount = math.ZeroInt()
	if c := leg.GetCoin(); c != nil {
		set++
		key = c.Denom
		amount = c.Amount
	}
	if v := leg.GetSvc(); v != nil {
		set++
		key = "svc/" + v.Schema
		amount = v.Units
	}
	if d := leg.GetData(); d != nil {
		set++
		key = "data/" + hex.EncodeToString(d.ContentHash)
		amount = math.OneInt()
	}
	if set != 1 {
		return "", math.ZeroInt(), false
	}
	return key, amount, true
}

// IsCoinDenom reports whether an asset key names a fungible coin (the only kind
// that can serve as a pricing numeraire): svc/ and data/ keys are not coins.
func IsCoinDenom(key string) bool {
	return key != "" && !strings.HasPrefix(key, "svc/") && !strings.HasPrefix(key, "data/")
}

// LegToSettle converts a give/want leg into a directed settlement leg (same
// asset), from the giver sigil to the receiver sigil. Mirrors the chain's
// intentLegToSettle (fallback.go).
func LegToSettle(from, to string, leg veiltypes.IntentLeg) sessiontypes.SettleLeg {
	out := sessiontypes.SettleLeg{FromSigil: from, ToSigil: to}
	switch {
	case leg.GetCoin() != nil:
		c := *leg.GetCoin()
		out.Asset = &sessiontypes.SettleLeg_Coin{Coin: &c}
	case leg.GetSvc() != nil:
		v := *leg.GetSvc()
		out.Asset = &sessiontypes.SettleLeg_Svc{Svc: &v}
	case leg.GetData() != nil:
		d := *leg.GetData()
		out.Asset = &sessiontypes.SettleLeg_Data{Data: &d}
	}
	return out
}

// LegToSettleAmount is the PARTIAL-FILL form of LegToSettle (task 2.6): the
// settlement leg carries `amount` of the leg's asset instead of the whole
// offered quantity, so a giver that signed `allow_partial` can hand over part of
// its offer and keep the remainder. A DataGrant has no divisible quantity — its
// conservation amount is the bookkeeping 1 (D-048) — so a data leg is only ever
// emitted whole; ok is false if a different amount is asked of one, or if the leg
// names no asset.
func LegToSettleAmount(from, to string, leg veiltypes.IntentLeg, amount math.Int) (sessiontypes.SettleLeg, bool) {
	out := sessiontypes.SettleLeg{FromSigil: from, ToSigil: to}
	switch {
	case leg.GetCoin() != nil:
		c := *leg.GetCoin()
		c.Amount = amount
		out.Asset = &sessiontypes.SettleLeg_Coin{Coin: &c}
	case leg.GetSvc() != nil:
		v := *leg.GetSvc()
		v.Units = amount
		out.Asset = &sessiontypes.SettleLeg_Svc{Svc: &v}
	case leg.GetData() != nil:
		if !amount.Equal(math.OneInt()) {
			return sessiontypes.SettleLeg{}, false
		}
		d := *leg.GetData()
		out.Asset = &sessiontypes.SettleLeg_Data{Data: &d}
	default:
		return sessiontypes.SettleLeg{}, false
	}
	return out, true
}
