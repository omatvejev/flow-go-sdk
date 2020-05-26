// (c) 2019 Dapper Labs - ALL RIGHTS RESERVED

package flow

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// A ChainID is a unique identifier for a specific Flow network instance.
//
// Chain IDs are used used to prevent replay attacks and to support network-specific address generation.
type ChainID string

// Mainnet is the chain ID for the mainnet node chain.
const Mainnet ChainID = "flow-mainnet"

// Testnet is the chain ID for the testnet node chain.
const Testnet ChainID = "flow-testnet"

// Emulator is the chain ID for the emulated node chain.
const Emulator ChainID = "flow-emulator"

func (id ChainID) String() string {
	return string(id)
}

// Address represents the 8 byte address of an account.
type Address [AddressLength]byte

// AddressState represents the internal state of the address generation mechanism
type AddressState uint64

const (
	// AddressLength is the size of an account address in bytes.
	// (n) is the size of an account address in bits.
	AddressLength = (linearCodeN + 7) >> 3
	// AddressStateLength is the size of an account address state in bytes.
	// (k) is the size of an account address in bits.
	AddressStateLength = (linearCodeK + 7) >> 3

	// ZeroAddressState is the addressing state when Flow is bootstrapped
	ZeroAddressState = AddressState(0)
	// ServiceAddressState is the initial addressing state for account creations
	ServiceAddressState = AddressState(1)
)

// chainCustomizer derives the constant used to generate addresses for 
// the given chain.
func chainCustomizer(chain ChainID) uint64 {
	switch chain {
	case Mainnet:
		return 0
	case Testnet:
		return invalidCodeTestnet
	case Emulator:
		return invalidCodeEmulator
	default:
		panic("chain ID is invalid")
	}
}

// ZeroAddress represents the "zero address" (account that no one owns).
func ZeroAddress(chain ChainID) Address {
	return generateAddress(chain, ZeroAddressState)
}

// ServiceAddress represents the root (first) generated account address.
func ServiceAddress(chain ChainID) Address {
	return generateAddress(chain, ServiceAddressState)
}

// HexToAddress converts a hex string to an Address.
func HexToAddress(h string) Address {
	b, _ := hex.DecodeString(h)
	return BytesToAddress(b)
}

// BytesToAddress returns Address with value b.
//
// If b is larger than 8, b will be cropped from the left.
// If b is smaller than 8, b will be appended by zeroes at the front.
func BytesToAddress(b []byte) Address {
	var a Address
	if len(b) > AddressLength {
		b = b[len(b)-AddressLength:]
	}
	copy(a[AddressLength-len(b):], b)
	return a
}

// Bytes returns the byte representation of the address.
func (a Address) Bytes() []byte { return a[:] }

// Hex returns the hex string representation of the address.
func (a Address) Hex() string {
	return hex.EncodeToString(a.Bytes())
}

// String returns the string representation of the address.
func (a Address) String() string {
	return a.Hex()
}

func (a Address) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", a.Hex())), nil
}

func (a *Address) UnmarshalJSON(data []byte) error {
	*a = HexToAddress(strings.Trim(string(data), "\""))
	return nil
}

const (
	// [n,k,d]-Linear code parameters
	// The linear code used in the account addressing is a [64,45,7]
	// It generates a [64,45]-code, which is the space of Flow account addresses.
	//
	// n is the size of the code words in bits,
	// which is also the size of the account addresses in bits.
	linearCodeN = 64
	// k is size of the words in bits.
	// 2^k is the total number of possible account addresses.
	linearCodeK = 45
	// p is the number of code parity bits.
	// p = n - k
	//
	// d is the distance of the linear code.
	// It is the minimum hamming distance between any two Flow account addresses.
	// This means any pair of Flow addresses have at least 7 different bits, which
	// minimizes the mistakes of typing wrong addresses.
	// d is also the minimum hamming weight of all account addresses (the zero address is not an account address).
	linearCodeD = 7

	// the maximum value of the internal state, 2^k.
	maxState = (1 << linearCodeK) - 1
)

// AccountAddress generates an account address given an addressing state.
//
// The address is generated for a specific network (Flow mainnet, testnet..)
// The second returned value is the new updated addressing state. The new
// addressing state should replace the old state to keep generating account
// addresses in a sequential way.
// Each state is mapped to exactly one address. There are as many addresses
// as states.
// ZeroAddress(chain) corresponds to the state "0" while ServiceAddress() corresponds to the
// state "1".
func (state *AddressState) AccountAddress(chain ChainID) (Address, error) {
	err := state.nextState()
	if err != nil {
		return ZeroAddress(chain), err
	}
	address := generateAddress(chain, *state)
	return address, nil
}

// returns the next state given an addressing state.
// The state values are incremented from 0 to 2^k-1
func (state *AddressState) nextState() error {
	if uint64(*state) > maxState {
		return fmt.Errorf("the state value is not valid, it must be less or equal to %x", maxState)
	}
	*state += 1
	return nil
}

// Uint64ToAddress returns an address with value v
// The value v fits into the address as the address size is 8
func Uint64ToAddress(v uint64) Address {
	var b [AddressLength]byte
	binary.BigEndian.PutUint64(b[:], v)
	return Address(b)
}

// uint64 converts an address into a uint64
func (a *Address) Uint64() uint64 {
	v := binary.BigEndian.Uint64(a[:])
	return v
}

// generateAddress returns an account address given an addressing state.
// (network) specifies the network to generate the address for (Flow Mainnet, testent..)
// The function assumes the state is valid (<2^k) which means
// a check on the state should be done before calling this function.
func generateAddress(chain ChainID, state AddressState) Address {
	index := uint64(state)

	// Multiply the index GF(2) vector by the code generator matrix
	address := uint64(0)
	for i := 0; i < linearCodeK; i++ {
		if index&1 == 1 {
			address ^= generatorMatrixRows[i]
		}
		index >>= 1
	}

	// customize the code word for a specific network
	address ^= chainCustomizer(chain)
	return Uint64ToAddress(address)
}

// IsValid returns true if a given address is a valid account address,
// and false otherwise.
//
// This is an off-chain check that only tells whether the address format is
// valid. If the function returns true, this does not mean
// a Flow account with this address has been generated. Such a test would
// require on on-chain check.
// ZeroAddress fails the check. Although it has a valid format, no account
// in Flow is assigned to ZeroAddress.
func (a *Address) IsValid(chain ChainID) bool {
	codeWord := a.Uint64()
	codeWord ^= chainCustomizer(chain)

	if codeWord == 0 {
		return false
	}

	// Multiply the code word GF(2)-vector by the parity-check matrix
	parity := uint(0)
	for i := 0; i < linearCodeN; i++ {
		if codeWord&1 == 1 {
			parity ^= parityCheckMatrixColumns[i]
		}
		codeWord >>= 1
	}
	return parity == 0
}

// invalid code-words in the [64,45] code
// these constants are used to generate non-Flow-Mainnet addresses
const invalidCodeTestnet = uint64(0x6834ba37b3980209)
const invalidCodeEmulator = uint64(0x1cb159857af02018)

// Rows of the generator matrix G of the [64,45]-code used for Flow addresses.
// G is a (k x n) matrix with coefficients in GF(2), each row is converted into
// a big endian integer representation of the GF(2) raw vector.
// G is used to generate the account addresses
var generatorMatrixRows = [linearCodeK]uint64{
	0xe467b9dd11fa00df, 0xf233dcee88fe0abe, 0xf919ee77447b7497, 0xfc8cf73ba23a260d,
	0xfe467b9dd11ee2a1, 0xff233dcee888d807, 0xff919ee774476ce6, 0x7fc8cf73ba231d10,
	0x3fe467b9dd11b183, 0x1ff233dcee8f96d6, 0x8ff919ee774757ba, 0x47fc8cf73ba2b331,
	0x23fe467b9dd27f6c, 0x11ff233dceee8e82, 0x88ff919ee775dd8f, 0x447fc8cf73b905e4,
	0xa23fe467b9de0d83, 0xd11ff233dce8d5a7, 0xe88ff919ee73c38a, 0x7447fc8cf73f171f,
	0xba23fe467b9dcb2b, 0xdd11ff233dcb0cb4, 0xee88ff919ee26c5d, 0x77447fc8cf775dd3,
	0x3ba23fe467b9b5a1, 0x9dd11ff233d9117a, 0xcee88ff919efa640, 0xe77447fc8cf3e297,
	0x73ba23fe467fabd2, 0xb9dd11ff233fb16c, 0xdcee88ff919adde7, 0xee77447fc8ceb196,
	0xf73ba23fe4621cd0, 0x7b9dd11ff2379ac3, 0x3dcee88ff91df46c, 0x9ee77447fc88e702,
	0xcf73ba23fe4131b6, 0x67b9dd11ff240f9a, 0x33dcee88ff90f9e0, 0x19ee77447fcff4e3,
	0x8cf73ba23fe64091, 0x467b9dd11ff115c7, 0x233dcee88ffdb735, 0x919ee77447fe2309,
	0xc8cf73ba23fdc736}

// Columns of the parity-check matrix H of the [64,45]-code used for Flow addresses.
// H is a (n x p) matrix with coefficients in GF(2), each column is converted into
// a big endian integer representation of the GF(2) column vector.
// H is used to verify a code word is a valid account address.
var parityCheckMatrixColumns = [linearCodeN]uint{
	0x00001, 0x00002, 0x00004, 0x00008,
	0x00010, 0x00020, 0x00040, 0x00080,
	0x00100, 0x00200, 0x00400, 0x00800,
	0x01000, 0x02000, 0x04000, 0x08000,
	0x10000, 0x20000, 0x40000, 0x7328d,
	0x6689a, 0x6112f, 0x6084b, 0x433fd,
	0x42aab, 0x41951, 0x233ce, 0x22a81,
	0x21948, 0x1ef60, 0x1deca, 0x1c639,
	0x1bdd8, 0x1a535, 0x194ac, 0x18c46,
	0x1632b, 0x1529b, 0x14a43, 0x13184,
	0x12942, 0x118c1, 0x0f812, 0x0e027,
	0x0d00e, 0x0c83c, 0x0b01d, 0x0a831,
	0x0982b, 0x07034, 0x0682a, 0x05819,
	0x03807, 0x007d2, 0x00727, 0x0068e,
	0x0067c, 0x0059d, 0x004eb, 0x003b4,
	0x0036a, 0x002d9, 0x001c7, 0x0003f,
}
