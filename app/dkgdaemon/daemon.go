// Package dkgdaemon is the off-chain DKG daemon (Task 1.7, D-070) — the missing
// client that drives the x/epoch on-chain DKG transport on a live node. It is the
// piece 1.5/1.6 stubbed (harness-seeded shares): it watches FORMING epochs, runs
// the kyber DKG rounds locally, submits the register/deal/response/justify/confirm
// txs to its own node, and PERSISTS the resulting node-local secret share into the
// file-backed ShareProvider so ExtendVote can reveal under it.
//
// It is launched IN-PROCESS from `conclaved start` (server StartCmdOptions
// PostSetup) so the secret share is written straight to the node's local store
// and the validator signing key never leaves the node process (the D1 trust
// boundary, D-070). The secret share NEVER enters consensus state or the p2p
// network (D-061/D-062); only public bundles transit chain state.
package dkgdaemon

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/AryaSingh22/conclave/app/dkgshare"
	"github.com/AryaSingh22/conclave/crypto/threshold"
	epochtypes "github.com/AryaSingh22/conclave/x/epoch/types"
)

// Daemon drives this node's participation in every FORMING epoch's DKG.
type Daemon struct {
	clientCtx client.Context
	logger    log.Logger

	valoper        string // this node's concvaloper... (committee identity)
	fromName       string // keyring key name to sign DKG txs with
	fromAddr       sdk.AccAddress
	longTermSecret []byte // node-local long-term DKG secret (BLS12-381 G2 scalar)
	longTermPublic []byte // its marshaled public (registered on-chain)

	shares *dkgshare.FileProvider

	// faultyDeal makes this node submit a corrupt (but authentic) deal and refuse
	// to justify — the adversarial faulty-dealer mode the localnet exercises to
	// prove the justification round excludes it without halting the chain.
	faultyDeal bool

	poll time.Duration
	runs map[uint64]*epochRun
}

// epochRun is this node's in-flight DKG state for one FORMING epoch (the kyber
// participant persists across windows/ticks).
type epochRun struct {
	part        *threshold.DKGParticipant
	myIndex     int
	inCommittee bool

	registered bool
	built      bool
	dealt      bool
	responded  bool
	justified  bool
	confirmed  bool
	done       bool

	material *threshold.EpochKeyMaterial
}

// Config carries the daemon's node-local inputs.
type Config struct {
	Valoper        string
	FromName       string
	FromAddr       sdk.AccAddress
	LongTermSecret []byte
	Shares         *dkgshare.FileProvider
	FaultyDeal     bool
	PollInterval   time.Duration
}

// New builds a daemon. It derives and caches the long-term public from the
// secret so registration is self-consistent with the stored key.
func New(clientCtx client.Context, logger log.Logger, cfg Config) (*Daemon, error) {
	pub, err := threshold.LongTermPublicFromSecret(cfg.LongTermSecret)
	if err != nil {
		return nil, fmt.Errorf("dkgdaemon: derive long-term public: %w", err)
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}
	return &Daemon{
		clientCtx:      clientCtx.WithFromName(cfg.FromName).WithFromAddress(cfg.FromAddr).WithBroadcastMode(flags.BroadcastSync),
		logger:         logger.With("module", "dkg-daemon", "valoper", cfg.Valoper),
		valoper:        cfg.Valoper,
		fromName:       cfg.FromName,
		fromAddr:       cfg.FromAddr,
		longTermSecret: cfg.LongTermSecret,
		longTermPublic: pub,
		shares:         cfg.Shares,
		faultyDeal:     cfg.FaultyDeal,
		poll:           poll,
		runs:           map[uint64]*epochRun{},
	}, nil
}

// Run polls until ctx is cancelled, driving the DKG for each FORMING epoch.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("DKG daemon started", "faulty", d.faultyDeal, "poll", d.poll)
	t := time.NewTicker(d.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("DKG daemon stopping")
			return nil
		case <-t.C:
			if err := d.tick(ctx); err != nil {
				d.logger.Debug("dkg tick", "err", err)
			}
		}
	}
}

// tick advances the DKG for the current FORMING epoch by one step (idempotent;
// each round's tx is submitted once).
func (d *Daemon) tick(ctx context.Context) error {
	q := epochtypes.NewQueryClient(d.clientCtx)

	formingResp, err := q.FormingEpoch(ctx, &epochtypes.QueryFormingEpochRequest{})
	if err != nil {
		return err
	}
	e := formingResp.Epoch
	if e.Id == 0 {
		return nil // nothing forming
	}

	pResp, err := q.Params(ctx, &epochtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	p := pResp.Params

	h, err := d.height(ctx)
	if err != nil {
		return err
	}

	run := d.runs[e.Id]
	if run == nil {
		run = &epochRun{}
		d.runs[e.Id] = run
	}
	if run.done {
		return nil
	}

	regDeadline := e.FormedHeight + p.RegisterWindowBlocks
	dealDeadline := e.DealDeadline
	respDeadline := e.DealDeadline + p.ResponseWindowBlocks
	justDeadline := respDeadline + p.JustifyWindowBlocks

	switch {
	case h < regDeadline:
		return d.doRegister(ctx, q, e, run)
	case h < dealDeadline:
		if err := d.ensureParticipant(ctx, q, e, run); err != nil || !run.inCommittee {
			return err
		}
		return d.doDeal(ctx, e, run)
	case h < respDeadline:
		if err := d.ensureParticipant(ctx, q, e, run); err != nil || !run.inCommittee {
			return err
		}
		return d.doResponse(ctx, q, e, run)
	case h < justDeadline:
		if err := d.ensureParticipant(ctx, q, e, run); err != nil || !run.inCommittee {
			return err
		}
		return d.doJustify(ctx, q, e, run)
	case h < e.ConfirmDeadline:
		if err := d.ensureParticipant(ctx, q, e, run); err != nil || !run.inCommittee {
			return err
		}
		return d.doConfirm(ctx, q, e, run)
	default:
		run.done = true
	}
	return nil
}

// doRegister submits this node's long-term DKG key once during the register window.
func (d *Daemon) doRegister(ctx context.Context, q epochtypes.QueryClient, e epochtypes.Epoch, run *epochRun) error {
	if run.registered {
		return nil
	}
	// already registered (e.g. after restart)?
	regs, err := q.Registrations(ctx, &epochtypes.QueryRegistrationsRequest{EpochId: e.Id})
	if err != nil {
		return err
	}
	for _, r := range regs.Registrations {
		if r.Valoper == d.valoper {
			run.registered = true
			return nil
		}
	}
	if err := d.broadcast(ctx, &epochtypes.MsgRegisterDKGKey{
		Sender: d.fromAddr.String(), EpochId: e.Id, DkgPubKey: d.longTermPublic,
	}); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	d.logger.Info("registered DKG key", "epoch", e.Id)
	run.registered = true
	return nil
}

// ensureParticipant builds this node's kyber participant once the committee is
// finalized (after the register window). If this node is not in the finalized
// committee it marks the run done (it did not register / was excluded).
func (d *Daemon) ensureParticipant(ctx context.Context, q epochtypes.QueryClient, e epochtypes.Epoch, run *epochRun) error {
	if run.built {
		return nil
	}
	if len(e.Committee) == 0 {
		return fmt.Errorf("committee not finalized yet for epoch %d", e.Id)
	}
	myIndex := -1
	for _, m := range e.Committee {
		if m.Valoper == d.valoper {
			myIndex = int(m.DkgIndex)
		}
	}
	if myIndex < 0 {
		d.logger.Info("not in finalized DKG committee — sitting this epoch out", "epoch", e.Id)
		run.built = true
		run.done = true
		return nil
	}
	regs, err := q.Registrations(ctx, &epochtypes.QueryRegistrationsRequest{EpochId: e.Id})
	if err != nil {
		return err
	}
	// the committee is the sorted registrants, so registrations[i] aligns with
	// committee dkg_index i — the roster the DKG deals against.
	roster := make([][]byte, len(regs.Registrations))
	for i, r := range regs.Registrations {
		roster[i] = r.DkgPubKey
	}
	part, err := threshold.NewParticipantFromRoster(e.Id, myIndex, d.longTermSecret, roster, int(e.Threshold))
	if err != nil {
		return fmt.Errorf("build participant: %w", err)
	}
	run.part = part
	run.myIndex = myIndex
	run.inCommittee = true
	run.built = true
	d.logger.Info("DKG committee finalized", "epoch", e.Id, "my_index", myIndex, "n", len(e.Committee), "t", e.Threshold)
	return nil
}

// doDeal submits this node's deal bundle once during the deal window.
func (d *Daemon) doDeal(ctx context.Context, e epochtypes.Epoch, run *epochRun) error {
	if run.dealt {
		return nil
	}
	deal, err := run.part.Round1Deal()
	if err != nil {
		return fmt.Errorf("deal: %w", err)
	}
	if d.faultyDeal {
		threshold.CorruptDealForTest(run.part, deal) // adversarial: authentic-but-bad deal
		d.logger.Info("ADVERSARY: submitting a faulty deal", "epoch", e.Id)
	}
	enc, err := threshold.MarshalDealBundle(deal)
	if err != nil {
		return err
	}
	if err := d.broadcast(ctx, &epochtypes.MsgSubmitDeal{
		Sender: d.fromAddr.String(), EpochId: e.Id, DkgIndex: uint32(run.myIndex), Bundle: enc,
	}); err != nil {
		return fmt.Errorf("submit deal: %w", err)
	}
	run.dealt = true
	return nil
}

// doResponse processes all committed deals and submits a response (complaint)
// bundle once during the response window, if this node has anything to report.
func (d *Daemon) doResponse(ctx context.Context, q epochtypes.QueryClient, e epochtypes.Epoch, run *epochRun) error {
	if run.responded {
		return nil
	}
	deals, err := d.deals(ctx, q, e.Id)
	if err != nil {
		return err
	}
	run.responded = true // process deals exactly once regardless of outcome
	resp, err := run.part.Round2Response(deals)
	if err != nil {
		d.logger.Error("process deals", "epoch", e.Id, "err", err)
		return nil
	}
	if resp == nil {
		return nil // no complaints
	}
	enc, err := threshold.MarshalResponseBundle(resp)
	if err != nil {
		return err
	}
	if err := d.broadcast(ctx, &epochtypes.MsgSubmitResponse{
		Sender: d.fromAddr.String(), EpochId: e.Id, DkgIndex: uint32(run.myIndex), Bundle: enc,
	}); err != nil {
		return fmt.Errorf("submit response: %w", err)
	}
	d.logger.Info("submitted DKG complaint(s)", "epoch", e.Id)
	return nil
}

// doJustify processes the responses; if this node is a complained-about dealer it
// submits a justification (unless it is the adversary refusing to justify). On the
// all-honest path the material is produced here directly.
func (d *Daemon) doJustify(ctx context.Context, q epochtypes.QueryClient, e epochtypes.Epoch, run *epochRun) error {
	if run.justified {
		return nil
	}
	resps, err := d.responses(ctx, q, e.Id)
	if err != nil {
		return err
	}
	run.justified = true
	material, jb, err := run.part.ProcessResponses(resps)
	if err != nil {
		d.logger.Error("process responses", "epoch", e.Id, "err", err)
		return nil
	}
	if material != nil {
		run.material = material // all-honest path: no justify window needed
		return nil
	}
	if jb == nil {
		return nil // waiting on others' justifications
	}
	if d.faultyDeal {
		d.logger.Info("ADVERSARY: refusing to justify (will be excluded)", "epoch", e.Id)
		return nil
	}
	enc, err := threshold.MarshalJustificationBundle(jb)
	if err != nil {
		return err
	}
	if err := d.broadcast(ctx, &epochtypes.MsgSubmitJustification{
		Sender: d.fromAddr.String(), EpochId: e.Id, DkgIndex: uint32(run.myIndex), Bundle: enc,
	}); err != nil {
		return fmt.Errorf("submit justification: %w", err)
	}
	d.logger.Info("submitted DKG justification", "epoch", e.Id)
	return nil
}

// doConfirm finalizes the DKG (over justifications if needed), PERSISTS the
// node-local secret share, and confirms the epoch key once during the confirm
// window.
func (d *Daemon) doConfirm(ctx context.Context, q epochtypes.QueryClient, e epochtypes.Epoch, run *epochRun) error {
	if run.confirmed {
		return nil
	}
	if run.material == nil {
		justs, err := d.justifications(ctx, q, e.Id)
		if err != nil {
			return err
		}
		mat, err := run.part.Finalize(justs)
		if err != nil {
			d.logger.Error("finalize DKG (excluded / failed)", "epoch", e.Id, "err", err)
			run.confirmed = true
			run.done = true
			return nil
		}
		run.material = mat
	}
	// PERSIST the node-local share BEFORE confirming, so the moment the epoch goes
	// ACTIVE this node can already reveal under it.
	if err := d.shares.Store(e.Id, uint32(run.material.Index), run.material.Share); err != nil {
		return fmt.Errorf("persist share: %w", err)
	}
	commits, err := threshold.MarshalCommits(run.material.Commits)
	if err != nil {
		return err
	}
	if err := d.broadcast(ctx, &epochtypes.MsgConfirmEpochKey{
		Sender: d.fromAddr.String(), EpochId: e.Id, DkgIndex: uint32(run.material.Index),
		EpochPubKey: run.material.EpochPub, Commits: commits,
	}); err != nil {
		return fmt.Errorf("confirm: %w", err)
	}
	d.logger.Info("confirmed epoch key + persisted node-local share", "epoch", e.Id, "index", run.material.Index)
	run.confirmed = true
	run.done = true
	return nil
}
