package hedera

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"

	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/pbkdf2"

	"github.com/hashgraph/hedera-sdk-go/v2/proto"
	"github.com/youmark/pkcs8"
)

const ed25519PrivateKeyPrefix = "302e020100300506032b657004220420"
const ed25519PubKeyPrefix = "302a300506032b6570032100"

type Key interface {
	toProtoKey() *proto.Key
	String() string
}

func keyFromProtobuf(pbKey *proto.Key) (Key, error) {
	switch key := pbKey.GetKey().(type) {
	case *proto.Key_Ed25519:
		return PublicKeyFromBytes(key.Ed25519)

	case *proto.Key_ThresholdKey:
		threshold := int(key.ThresholdKey.GetThreshold())
		keys, err := keyListFromProtobuf(key.ThresholdKey.GetKeys())
		if err != nil {
			return nil, err
		}
		keys.threshold = threshold

		return &keys, nil

	case *proto.Key_KeyList:
		keys, err := keyListFromProtobuf(key.KeyList)
		if err != nil {
			return nil, err
		}

		return &keys, nil

	case *proto.Key_ContractID:
		keys := contractIDFromProtobuf(key.ContractID)
		return keys, nil

	default:
		return nil, newErrBadKeyf("key type not implemented: %v", key)
	}
}

// PrivateKey is an ed25519 private key.
type PrivateKey struct {
	keyData   []byte
	chainCode []byte
}

// PublicKey is an ed25519 public key.
type PublicKey struct {
	keyData []byte
}

// GeneratePrivateKey generates a random new PrivateKey.
func GeneratePrivateKey() (PrivateKey, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, err
	}

	return PrivateKey{
		keyData: privateKey,
	}, nil
}

// PrivateKeyFromBytes constructs an PrivateKey from a raw slice of either 32 or 64 bytes.
func PrivateKeyFromBytes(bytes []byte) (PrivateKey, error) {
	length := len(bytes)
	if length != 32 && length != 64 {
		return PrivateKey{}, newErrBadKeyf("invalid private key length: %v bytes", len(bytes))
	}

	return PrivateKey{
		keyData: ed25519.NewKeyFromSeed(bytes[0:32]),
	}, nil
}

// PrivateKeyFromMnemonic recovers an PrivateKey from a valid 24 word length mnemonic phrase and a
// passphrase.
//
// An empty string can be passed for passPhrase If the mnemonic phrase wasn't generated with a passphrase. This is
// required to recover a private key from a mnemonic generated by the Android and iOS wallets.
func PrivateKeyFromMnemonic(mnemonic Mnemonic, passPhrase string) (PrivateKey, error) {
	salt := []byte("mnemonic" + passPhrase)
	seed := pbkdf2.Key([]byte(mnemonic.String()), salt, 2048, 64, sha512.New)

	h := hmac.New(sha512.New, []byte("ed25519 seed"))

	_, err := h.Write(seed)
	if err != nil {
		return PrivateKey{}, err
	}

	digest := h.Sum(nil)

	keyBytes := digest[0:32]
	chainCode := digest[32:]

	// note the index is for derivation, not the index of the slice
	for _, index := range []uint32{44, 3030, 0, 0} {
		keyBytes, chainCode = deriveChildKey(keyBytes, chainCode, index)
	}

	privateKey, err := PrivateKeyFromBytes(keyBytes)

	if err != nil {
		return PrivateKey{}, err
	}

	privateKey.chainCode = chainCode

	return privateKey, nil
}

// PrivateKeyFromString recovers an PrivateKey from its text-encoded representation.
func PrivateKeyFromString(s string) (PrivateKey, error) {
	sLen := len(s)
	if sLen != 64 && sLen != 96 && sLen != 128 {
		return PrivateKey{}, newErrBadKeyf("invalid private key string with length %v", len(s))
	}

	bytes, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), ed25519PrivateKeyPrefix))
	if err != nil {
		return PrivateKey{}, err
	}

	return PrivateKeyFromBytes(bytes)
}

// PrivateKeyFromKeystore recovers an PrivateKey from an encrypted keystore encoded as a byte slice.
func PrivateKeyFromKeystore(ks []byte, passphrase string) (PrivateKey, error) {
	return parseKeystore(ks, passphrase)
}

// PrivateKeyReadKeystore recovers an PrivateKey from an encrypted keystore file.
func PrivateKeyReadKeystore(source io.Reader, passphrase string) (PrivateKey, error) {
	keystoreBytes, err := ioutil.ReadAll(source)
	if err != nil {
		return PrivateKey{}, err
	}

	return PrivateKeyFromKeystore(keystoreBytes, passphrase)
}

func PrivateKeyFromPem(bytes []byte, passphrase string) (PrivateKey, error) {
	var blockType string

	if len(passphrase) == 0 {
		blockType = "PRIVATE KEY"
	} else {
		// the pem is encrypted
		blockType = "ENCRYPTED PRIVATE KEY"
	}

	var pk *pem.Block
	for block, rest := pem.Decode(bytes); block != nil; {
		if block.Type == blockType {
			pk = block
			break
		}

		bytes = rest
		if len(bytes) == 0 {
			// no key was found
			return PrivateKey{}, newErrBadKeyf("pem file did not contain a private key")
		}
	}

	if len(passphrase) == 0 {
		// key does not need decrypted, end here
		return PrivateKeyFromString(hex.EncodeToString(pk.Bytes))
	}

	keyI, err := pkcs8.ParsePKCS8PrivateKey(pk.Bytes, []byte(passphrase))
	if err != nil {
		return PrivateKey{}, err
	}

	return PrivateKeyFromBytes(keyI.(ed25519.PrivateKey))
}

func PrivateKeyReadPem(source io.Reader, passphrase string) (PrivateKey, error) {
	// note: Passphrases are currently not supported, but included in the function definition to avoid breaking
	// changes in the future.

	pemFileBytes, err := ioutil.ReadAll(source)
	if err != nil {
		return PrivateKey{}, err
	}

	return PrivateKeyFromPem(pemFileBytes, passphrase)
}

// PublicKeyFromString recovers an PublicKey from its text-encoded representation.
func PublicKeyFromString(s string) (PublicKey, error) {
	sLen := len(s)
	if sLen != 64 && sLen != 88 {
		return PublicKey{}, newErrBadKeyf("invalid public key '%v' string with length %v", s, sLen)
	}

	keyStr := strings.TrimPrefix(strings.ToLower(s), ed25519PubKeyPrefix)
	bytes, err := hex.DecodeString(keyStr)
	if err != nil {
		return PublicKey{}, err
	}

	return PublicKey{bytes}, nil
}

// PublicKeyFromBytes constructs a known PublicKey from its text-encoded representation.
func PublicKeyFromBytes(bytes []byte) (PublicKey, error) {
	if len(bytes) != ed25519.PublicKeySize {
		return PublicKey{}, newErrBadKeyf("invalid public key length: %v bytes", len(bytes))
	}

	return PublicKey{
		keyData: bytes,
	}, nil
}

// SLIP-10/BIP-32 Child Key derivation
func deriveChildKey(parentKey []byte, chainCode []byte, index uint32) ([]byte, []byte) {
	h := hmac.New(sha512.New, chainCode)

	input := make([]byte, 37)

	// 0x00 + parentKey + index(BE)
	input[0] = 0

	copy(input[1:37], parentKey)

	binary.BigEndian.PutUint32(input[33:37], index)

	// harden the input
	input[33] |= 128

	h.Write(input)
	digest := h.Sum(nil)

	return digest[0:32], digest[32:]
}

// PublicKey returns the PublicKey associated with this PrivateKey.
func (sk PrivateKey) PublicKey() PublicKey {
	return PublicKey{
		keyData: sk.keyData[32:],
	}
}

// String returns the text-encoded representation of the PrivateKey.
func (sk PrivateKey) String() string {
	return fmt.Sprint(ed25519PrivateKeyPrefix, hex.EncodeToString(sk.keyData[:32]))
}

// String returns the text-encoded representation of the PublicKey.
func (pk PublicKey) String() string {
	return fmt.Sprint(ed25519PubKeyPrefix, hex.EncodeToString(pk.keyData))
}

// Bytes returns the byte slice representation of the PrivateKey.
func (sk PrivateKey) Bytes() []byte {
	return sk.keyData
}

// Keystore returns an encrypted keystore containing the PrivateKey.
func (sk PrivateKey) Keystore(passphrase string) ([]byte, error) {
	return newKeystore(sk.keyData, passphrase)
}

// WriteKeystore writes an encrypted keystore containing the PrivateKey to the provided destination.
func (sk PrivateKey) WriteKeystore(destination io.Writer, passphrase string) error {
	keystore, err := sk.Keystore(passphrase)
	if err != nil {
		return err
	}

	_, err = destination.Write(keystore)

	return err
}

// Sign signs the provided message with the PrivateKey.
func (sk PrivateKey) Sign(message []byte) []byte {
	return ed25519.Sign(sk.keyData, message)
}

// SupportsDerivation returns true if the PrivateKey supports derivation.
func (sk PrivateKey) SupportsDerivation() bool {
	return sk.chainCode != nil
}

// Derive a child key compatible with the iOS and Android wallets using a provided wallet/account index. Use index 0 for
// the default account.
//
// This will fail if the key does not support derivation which can be checked by calling SupportsDerivation()
func (sk PrivateKey) Derive(index uint32) (PrivateKey, error) {
	if !sk.SupportsDerivation() {
		return PrivateKey{}, newErrBadKeyf("child key cannot be derived from this key")
	}

	derivedKeyBytes, chainCode := deriveChildKey(sk.Bytes(), sk.chainCode, index)

	derivedKey, err := PrivateKeyFromBytes(derivedKeyBytes)

	if err != nil {
		return PrivateKey{}, err
	}

	derivedKey.chainCode = chainCode

	return derivedKey, nil
}

// Bytes returns the byte slice representation of the PublicKey.
func (pk PublicKey) Bytes() []byte {
	return pk.keyData
}

func (sk PrivateKey) toProtoKey() *proto.Key {
	return sk.PublicKey().toProtoKey()
}

func (pk PublicKey) toProtoKey() *proto.Key {
	return &proto.Key{Key: &proto.Key_Ed25519{Ed25519: pk.keyData}}
}

func (pk PublicKey) toSignaturePairProtobuf(signature []byte) *proto.SignaturePair {
	return &proto.SignaturePair{
		PubKeyPrefix: pk.keyData,
		Signature: &proto.SignaturePair_Ed25519{
			Ed25519: signature,
		},
	}
}

func (sk PrivateKey) SignTransaction(transaction Transaction) ([]byte, error) {
	transaction.requireOneNodeAccountID()

	if len(transaction.transactions) == 0 {
		return make([]byte, 0), errTransactionRequiresSingleNodeAccountID
	}

	signature := sk.Sign(transaction.transactions[0].GetBodyBytes())
	transaction.AddSignature(sk.PublicKey(), signature)

	return signature, nil
}
