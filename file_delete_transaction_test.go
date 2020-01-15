package hedera

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSerializeFileDeleteTransaction(t *testing.T) {
	mockClient, err := newMockClient()
	assert.NoError(t, err)

	privateKey, err := Ed25519PrivateKeyFromString(mockPrivateKey)
	assert.NoError(t, err)

	tx := NewFileDeleteTransaction().
		SetFileID(FileID{File: 5}).
		SetMaxTransactionFee(1e6).
		SetTransactionID(testTransactionID).
		Build(&mockClient).
		Sign(privateKey)

	txString := `bodyBytes: "\n\016\n\010\010\334\311\007\020\333\237\t\022\002\030\003\022\002\030\003\030\300\204=\"\002\010x\222\001\004\022\002\030\005"
sigMap: <
  sigPair: <
    ed25519: "cX\335@\024\365\365\3065\211NT.\355\245\224\364\230@\301\221\343\\T\343H\374\003\261W\252a\272\3401-)\251?N\204\305C\034\301\375\306\327K7a` + "`" + `r\262]\247\231I\332*:\2432\010"
  >
>
transactionID: <
  transactionValidStart: <
    seconds: 124124
    nanos: 151515
  >
  accountID: <
    accountNum: 3
  >
>
nodeAccountID: <
  accountNum: 3
>
transactionFee: 1000000
transactionValidDuration: <
  seconds: 120
>
fileDelete: <
  fileID: <
    fileNum: 5
  >
>
`

	assert.Equal(t, txString, tx.String())
}
