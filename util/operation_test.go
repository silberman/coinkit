package util

import (
	"encoding/json"
	"testing"
)

type TestingOperation struct {
	Number int
	Signer string
}

func (op *TestingOperation) OperationType() string {
	return "Testing"
}

func (op *TestingOperation) String() string {
	return "Testing"
}

func (op *TestingOperation) GetSigner() string {
	return op.Signer
}

func (op *TestingOperation) Verify() bool {
	return true
}

func (op *TestingOperation) GetFee() uint64 {
	return 0
}

func (op *TestingOperation) GetSequence() uint32 {
	return 1
}

func init() {
	RegisterOperationType(&TestingOperation{})
}

// TODO: scrap below here

func TestOperationEncoding(t *testing.T) {
	op := &TestingOperation{Number: 5}
	op2 := EncodeThenDecodeOperation(op).(*TestingOperation)
	if op2.Number != 5 {
		t.Fatalf("op2.Number turned into %d", op2.Number)
	}
}

func TestDecodingInvalidOperation(t *testing.T) {
	bytes, err := json.Marshal(DecodedOperation{
		T: "Testing",
		O: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(bytes)
	op, err := DecodeOperation(encoded)
	if err == nil || op != nil {
		t.Fatal("an encoded nil operation should fail to decode")
	}
}
