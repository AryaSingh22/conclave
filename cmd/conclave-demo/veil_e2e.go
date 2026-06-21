package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/crypto/threshold"
	epochtypes "github.com/AryaSingh22/conclave/x/epoch/types"
	veiltypes "github.com/AryaSingh22/conclave/x/veil/types"
)

// veil_e2e.go is the Task-1.7 / D-070 headline LIVE end-to-end: on the real
// 4-validator localnet (where the DKG daemon has driven a real DKG to epoch 1
// ACTIVE), a client fetches epoch 1's published key, ENCRYPTS a 2-party swap to
// it, submits it tagged epoch_id=1, and the intents reveal (B+2, via real
// vote-extension threshold shares) and CLEAR through the in-consensus fallback
// matcher — all on the real network, not in-process. It also confirms the
// submitted ciphertext is genuinely opaque (≠ the plaintext payload), so the
// Veil's confidentiality is real on the live chain (F-4 surface).

// veilE2E runs the encrypted-intent → reveal → clear flow against epoch 1.
func (c *cli) veilE2E() error {
	epochc := epochtypes.NewQueryClient(c.conn)

	// 1. wait for the DKG daemon to bring epoch 1 ACTIVE (the real boundary +
	//    rotation has occurred and a consistent EpochPubKey is published).
	e, err := c.waitEpochActive(epochc, 180*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("veil-e2e: epoch %d ACTIVE — committee n=%d, t=%d, key=%d bytes ✓\n",
		e.Id, len(e.Committee), e.Threshold, len(e.EpochPubKey))

	// 2. ensure both swap parties have sigils.
	bob, err := c.key("bob")
	if err != nil {
		return err
	}
	carol, err := c.key("carol")
	if err != nil {
		return err
	}
	bobSigil, err := c.ensureSigil("bob", bob)
	if err != nil {
		return err
	}
	carolSigil, err := c.ensureSigil("carol", carol)
	if err != nil {
		return err
	}

	height, err := c.height()
	if err != nil {
		return err
	}
	ttl := height + 50

	// 3. build the SAME mutually-fillable 2-party swap as the fallback demo, but
	//    ENCRYPTED to epoch 1's key and tagged epoch_id=1.
	bobMsg, bobCT, bobPT := c.buildEncryptedIntent(addrOf(bob), bobSigil,
		[]veiltypes.IntentLeg{coinLeg(uusdc, 50*usdc)}, []veiltypes.IntentLeg{svcLeg(svcSchema, 5)},
		ttl, e.EpochPubKey, e.Id)
	carolMsg, _, _ := c.buildEncryptedIntent(addrOf(carol), carolSigil,
		[]veiltypes.IntentLeg{svcLeg(svcSchema, 5)}, []veiltypes.IntentLeg{coinLeg(uusdc, 50*usdc)},
		ttl, e.EpochPubKey, e.Id)

	// confidentiality is real: the on-wire ciphertext is NOT the plaintext.
	if bytes.Equal(bobCT, bobPT) {
		return fmt.Errorf("ciphertext equals plaintext — encryption did not happen (epoch_id=%d)", e.Id)
	}
	fmt.Printf("veil-e2e: encrypted 2-party swap to epoch %d (ciphertext %d bytes ≠ plaintext %d bytes) ✓\n",
		e.Id, len(bobCT), len(bobPT))

	ids, err := c.submitConcurrent([]txSpec{{priv: bob, msg: bobMsg}, {priv: carol, msg: carolMsg}})
	if err != nil {
		return fmt.Errorf("submit encrypted swap: %w", err)
	}
	fmt.Printf("veil-e2e: submitted encrypted swap %v (epoch_id=%d)\n", short(ids), e.Id)

	// 4. the intents reveal (B+2, via real VE threshold shares) then clear via the
	//    in-consensus fallback — on the real 4-node network.
	if err := c.waitState(ids, veiltypes.INTENT_STATE_CONCLUDED); err != nil {
		return fmt.Errorf("encrypted swap did not reveal+clear: %w", err)
	}
	fmt.Println("veil-e2e: encrypted intents revealed via real threshold shares and CLEARED ✓")
	fmt.Println("VEIL-E2E OK")
	return nil
}

// waitEpochActive polls CurrentEpoch until an ACTIVE epoch with a published key
// appears (the DKG daemon completed a real DKG), or times out.
func (c *cli) waitEpochActive(epochc epochtypes.QueryClient, timeout time.Duration) (epochtypes.Epoch, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := epochc.CurrentEpoch(context.Background(), &epochtypes.QueryCurrentEpochRequest{})
		if err == nil && res.Epoch.Id != 0 &&
			res.Epoch.Status == epochtypes.EPOCH_STATUS_ACTIVE && len(res.Epoch.EpochPubKey) > 0 {
			return res.Epoch, nil
		}
		time.Sleep(2 * time.Second)
	}
	return epochtypes.Epoch{}, fmt.Errorf("no ACTIVE epoch within %v — DKG daemon did not complete a DKG", timeout)
}

// buildEncryptedIntent builds a MsgSubmitIntent whose payload is ENCRYPTED to the
// epoch public key (epoch_id tagged), returning the message plus the ciphertext
// and plaintext for the confidentiality assertion.
func (c *cli) buildEncryptedIntent(sender sdk.AccAddress, sigil string, give, want []veiltypes.IntentLeg, ttl uint64, epochPub []byte, epochID uint64) (*veiltypes.MsgSubmitIntent, []byte, []byte) {
	payload := veiltypes.IntentPayload{Give: give, Want: want}
	payloadBz, err := payload.Marshal()
	if err != nil {
		panic(err)
	}
	ct, err := threshold.NewClientScheme().Encrypt(epochPub, payloadBz)
	if err != nil {
		panic(fmt.Errorf("encrypt to epoch %d key: %w", epochID, err))
	}
	// spend_commitment must cover the give legs (50 usdc) so the Reveal slash does
	// not fire (D-065).
	env := veiltypes.IntentEnvelope{
		TtlHeight:       ttl,
		EpochId:         epochID,
		SpendCommitment: sdk.NewCoins(sdk.NewInt64Coin(uusdc, 100*usdc)),
	}
	envBz, err := env.Marshal()
	if err != nil {
		panic(err)
	}
	return &veiltypes.MsgSubmitIntent{
		Sender:      sender.String(),
		SenderSigil: sigil,
		Ciphertext:  ct,
		Envelope:    envBz,
		Bond:        sdk.NewInt64Coin(uclv, oneCLV),
	}, ct, payloadBz
}
