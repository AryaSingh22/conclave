//! conclave-demo-escrow — the CVM-schema demo contract for the Phase-2 §6 exit
//! demo (D-079's deferred localnet contract-release smoke).
//!
//! WHY THIS CRATE EXISTS. Task 2.2 proved the CVM bindings end to end in
//! process, but its real-contract e2e had to use wasmd's `reflect` contract,
//! whose Custom message type is TYPED (`debug|raw`) and therefore **cannot
//! spell Conclave's custom-msg schema at all**. The live-network slice was
//! deferred to the exit demo "whose purpose-built CVM demo contract is its
//! natural vehicle" (D-079). This is that contract.
//!
//! WHAT IT IS AND IS NOT. It is DEV TOOLING: it is not a Go dependency, it is
//! absent from `go.mod`, it is not built by `make test`, and nothing it
//! produces enters `conclaved`. Its only job is to emit CVM custom messages
//! from inside a real wasm VM so the three-layer authority model (D-079) can be
//! exercised by a real network rather than a test harness.
//!
//! THE SCHEMA IS A CONTRACT. The variant names and field names below must match
//! `app/cvm/encoder.go` EXACTLY: that encoder decodes with
//! `DisallowUnknownFields` and requires exactly one branch to be set, so a
//! renamed field or an extra key is a hard rejection, not a silent no-op. The
//! Go side's `[]byte` fields are base64 in JSON, which is what `Binary`
//! serializes to.

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{
    entry_point, to_json_binary, Binary, CosmosMsg, CustomMsg, Deps, DepsMut, Env, MessageInfo,
    Response, StdResult,
};

pub const CONTRACT_NAME: &str = "conclave-demo-escrow";
pub const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

/// The CVM custom-message schema (`app/cvm/encoder.go`).
///
/// Three of these four branches are the allowlist. `Conclude` is deliberately
/// NOT in the allowlist and is deliberately present here: the demo needs a real
/// contract that genuinely tries to author settlement, so that layer 1 of the
/// D-079 authority model is watched rejecting a spelling that a live contract
/// actually emitted — rather than asserted from a unit test that never left Go.
#[cw_serde]
pub enum ConclaveMsg {
    /// HASHLOCK release / OPTIMISTIC reveal — knowledge-gated (D-073).
    ReleaseEscrow { escrow_id: String, preimage: Binary },
    /// ZK_DELIVERY release — proof-gated and instance-bound (D-076). The
    /// verifier rebuilds the public witness from the escrow RECORD, so this
    /// contract cannot influence which instance the proof is checked against.
    SubmitDeliveryProof { escrow_id: String, proof: Binary },
    /// OPTIMISTIC dispute — payer-identity-gated in the keeper.
    RaiseDispute { escrow_id: String },
    /// NOT ALLOWLISTED. Settlement authorship is module-authority-only; this
    /// exists so the demo can prove the rejection from a real contract.
    Conclude { session_id: u64 },
}

impl CustomMsg for ConclaveMsg {}

#[cw_serde]
pub struct InstantiateMsg {}

/// Each variant emits exactly one CVM custom message. The contract holds no
/// state and makes no decisions: it is a transparent conduit, so that anything
/// the demo observes is attributable to the BINDINGS, not to contract logic.
#[cw_serde]
pub enum ExecuteMsg {
    SubmitDeliveryProof { escrow_id: String, proof: Binary },
    ReleaseEscrow { escrow_id: String, preimage: Binary },
    RaiseDispute { escrow_id: String },
    /// Emits the non-allowlisted `conclude` branch (see `ConclaveMsg`).
    ForgeConclude { session_id: u64 },
}

#[cw_serde]
pub enum QueryMsg {
    Version {},
}

#[cw_serde]
pub struct VersionResponse {
    pub name: String,
    pub version: String,
}

#[entry_point]
pub fn instantiate(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    _msg: InstantiateMsg,
) -> StdResult<Response<ConclaveMsg>> {
    Ok(Response::new()
        .add_attribute("action", "instantiate")
        .add_attribute("contract", CONTRACT_NAME))
}

#[entry_point]
pub fn execute(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: ExecuteMsg,
) -> StdResult<Response<ConclaveMsg>> {
    let (action, cvm) = match msg {
        ExecuteMsg::SubmitDeliveryProof { escrow_id, proof } => (
            "submit_delivery_proof",
            ConclaveMsg::SubmitDeliveryProof { escrow_id, proof },
        ),
        ExecuteMsg::ReleaseEscrow {
            escrow_id,
            preimage,
        } => (
            "release_escrow",
            ConclaveMsg::ReleaseEscrow {
                escrow_id,
                preimage,
            },
        ),
        ExecuteMsg::RaiseDispute { escrow_id } => {
            ("raise_dispute", ConclaveMsg::RaiseDispute { escrow_id })
        }
        ExecuteMsg::ForgeConclude { session_id } => {
            ("forge_conclude", ConclaveMsg::Conclude { session_id })
        }
    };
    Ok(Response::new()
        .add_attribute("action", action)
        .add_message(CosmosMsg::Custom(cvm)))
}

#[entry_point]
pub fn query(_deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Version {} => to_json_binary(&VersionResponse {
            name: CONTRACT_NAME.to_string(),
            version: CONTRACT_VERSION.to_string(),
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use cosmwasm_std::testing::{message_info, mock_dependencies, mock_env};
    use cosmwasm_std::Addr;

    /// The wire shape is the contract with `app/cvm/encoder.go`. This asserts
    /// the exact JSON the encoder must accept — a rename on either side fails
    /// here rather than at demo time.
    #[test]
    fn submit_delivery_proof_serializes_to_the_cvm_schema() {
        let msg = ConclaveMsg::SubmitDeliveryProof {
            escrow_id: "abc123".to_string(),
            proof: Binary::new(vec![1, 2, 3]),
        };
        let json = serde_json::to_string(&msg).unwrap();
        assert_eq!(
            json,
            r#"{"submit_delivery_proof":{"escrow_id":"abc123","proof":"AQID"}}"#
        );
    }

    #[test]
    fn execute_emits_exactly_one_custom_message() {
        let mut deps = mock_dependencies();
        let info = message_info(&Addr::unchecked("sender"), &[]);
        let res = execute(
            deps.as_mut(),
            mock_env(),
            info,
            ExecuteMsg::SubmitDeliveryProof {
                escrow_id: "e1".to_string(),
                proof: Binary::new(vec![9]),
            },
        )
        .unwrap();
        assert_eq!(res.messages.len(), 1);
    }
}
