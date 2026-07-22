// hpke_demo.go
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/hex"
	"fmt"
	"log"
)

func main() {
	kem := hpke.DHKEM(ecdh.X25519())
	kdf := hpke.HKDFSHA256()
	aead := hpke.AES256GCM()

	// 1. Receiver (Service B) generates a key pair
	priv, err := kem.GenerateKey()
	if err != nil {
		log.Fatal(err)
	}
	pub := priv.PublicKey()

	fmt.Println("=== Service B (Receiver) ===")
	privBytes, _ := priv.Bytes()
	fmt.Printf("Private key: %s\n", hex.EncodeToString(privBytes))
	fmt.Printf("Public key:  %s\n\n", hex.EncodeToString(pub.Bytes()))

	// 2. Sender (Service A) encrypts to B's public key
	enc, sender, err := hpke.NewSender(pub, kdf, aead, nil)
	if err != nil {
		log.Fatal(err)
	}

	// The payload: a JSON request from A to B
	payload := []byte(`{"task": "embed", "text": "hello world", "model": "bge-small"}`)
	aad := []byte("service-a-to-b") // authenticated associated data (optional)

	// Seal returns: ciphertext
	ct, err := sender.Seal(aad, payload)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Service A (Sender) ===")
	fmt.Printf("Ciphertext (hex): %s\n\n", hex.EncodeToString(ct))

	// 3. Receiver opens the sealed message
	recipient, err := hpke.NewRecipient(enc, priv, kdf, aead, nil)
	if err != nil {
		log.Fatal(err)
	}

	pt, err := recipient.Open(aad, ct)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Service B (Receiver) Decrypts ===")
	fmt.Printf("Plaintext: %s\n", string(pt))

	// 4. Forward secrecy demo: new sender = new ephemeral key = new ciphertext
	enc2, sender2, _ := hpke.NewSender(pub, kdf, aead, nil)
	ct2, _ := sender2.Seal(aad, payload)
	fmt.Printf("\nSecond encryption (different ephemeral key):\n")
	fmt.Printf("Ciphertext differs: %v\n", !bytes.Equal(ct, ct2))
	_ = enc2
}
