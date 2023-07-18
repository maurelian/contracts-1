package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
)

func main() {
	var privateKey string
	var ledger bool
	var mnemonic string
	var hdPath string
	flag.StringVar(&privateKey, "private-key", "", "Private key to use for signing")
	flag.BoolVar(&ledger, "ledger", false, "Use ledger device for signing")
	flag.StringVar(&mnemonic, "mnemonic", "", "Mnemonic to use for signing")
	flag.StringVar(&hdPath, "hd-paths", "m/44'/60'/0'/0/0", "Hierarchical deterministic derivation path for mnemonic or ledger")
	flag.Parse()

	options := 0
	if privateKey != "" {
		options++
	}
	if ledger {
		options++
	}
	if mnemonic != "" {
		options++
	}
	if options != 1 {
		log.Fatalf("One (and only one) of --private-key, --ledger, --mnemonic must be set")
	}

	bytes, err := io.ReadAll(os.Stdin)
	fmt.Println()
	if err != nil {
		log.Fatalf("Error reading from stdin: %v", err)
	}
	hash := common.FromHex(strings.TrimSpace(string(bytes)))
	if len(hash) != 66 {
		log.Fatalf("Expected EIP-712 hex string with 66 bytes, got %d bytes, value: %s", len(bytes), string(bytes))
	}

	s, err := createSigner(privateKey, mnemonic, hdPath)
	if err != nil {
		log.Fatalf("Error creating signer: %v", err)
	}

	signature, err := s.sign(hash)
	if err != nil {
		log.Fatalf("Error signing data: %v", err)
	}

	fmt.Printf("Data: %s\n", hex.EncodeToString(hash))
	fmt.Printf("Signer: %s\n", s.address().String())
	fmt.Printf("Signature: %s\n", hex.EncodeToString(signature))
}

func createSigner(privateKey, mnemonic, hdPath string) (signer, error) {
	path, err := accounts.ParseDerivationPath(hdPath)
	if err != nil {
		return nil, err
	}

	if privateKey != "" {
		key, err := crypto.HexToECDSA(privateKey)
		if err != nil {
			return nil, fmt.Errorf("error parsing private key: %w", err)
		}
		return &ecdsaSigner{key}, nil
	}

	if mnemonic != "" {
		key, err := derivePrivateKey(mnemonic, path)
		if err != nil {
			return nil, fmt.Errorf("error deriving key from mnemonic: %w", err)
		}
		return &ecdsaSigner{key}, nil
	}

	// assume using a ledger
	ledgerHub, err := usbwallet.NewLedgerHub()
	if err != nil {
		return nil, fmt.Errorf("error starting ledger: %w", err)
	}
	wallets := ledgerHub.Wallets()
	if len(wallets) == 0 {
		return nil, fmt.Errorf("no ledgers found, please connect your ledger")
	} else if len(wallets) > 1 {
		return nil, fmt.Errorf("multiple ledgers found, please use one ledger at a time")
	}
	wallet := wallets[0]
	if err := wallet.Open(""); err != nil {
		return nil, fmt.Errorf("error opening ledger (have you unlocked?): %w", err)
	}
	account, err := wallet.Derive(path, true)
	if err != nil {
		return nil, fmt.Errorf("error deriving ledger account: %w", err)
	}
	return &walletSigner{
		wallet:  wallet,
		account: account,
	}, nil
}

type signer interface {
	address() common.Address
	sign([]byte) ([]byte, error)
}

type ecdsaSigner struct {
	*ecdsa.PrivateKey
}

func (s *ecdsaSigner) address() common.Address {
	return crypto.PubkeyToAddress(s.PublicKey)
}

func (s *ecdsaSigner) sign(data []byte) ([]byte, error) {
	sig, err := crypto.Sign(crypto.Keccak256(data), s.PrivateKey)
	if err != nil {
		return nil, err
	}
	sig[crypto.RecoveryIDOffset] += 27
	return sig, err
}

type walletSigner struct {
	wallet  accounts.Wallet
	account accounts.Account
}

func (s *walletSigner) address() common.Address {
	return s.account.Address
}

func (s *walletSigner) sign(data []byte) ([]byte, error) {
	return s.wallet.SignData(s.account, accounts.MimetypeTypedData, data)
}

func derivePrivateKey(mnemonic string, path accounts.DerivationPath) (*ecdsa.PrivateKey, error) {
	// Parse the seed string into the master BIP32 key.
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		return nil, err
	}

	privKey, err := hdkeychain.NewMaster(seed, fakeNetworkParams{})
	if err != nil {
		return nil, err
	}

	for _, child := range path {
		privKey, err = privKey.Child(child)
		if err != nil {
			return nil, err
		}
	}

	rawPrivKey, err := privKey.SerializedPrivKey()
	if err != nil {
		return nil, err
	}

	return crypto.ToECDSA(rawPrivKey)
}

type fakeNetworkParams struct{}

func (f fakeNetworkParams) HDPrivKeyVersion() [4]byte {
	return [4]byte{}
}

func (f fakeNetworkParams) HDPubKeyVersion() [4]byte {
	return [4]byte{}
}