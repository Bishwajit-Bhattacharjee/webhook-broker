package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	sqlite "github.com/mattn/go-sqlite3"
	"github.com/newscred/webhook-broker/storage/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	samplePayload      = "some payload"
	sampleContentType  = "a content type"
	duplicateMessageID = "a-duplicate-message-id"
)

var (
	producer1 *data.Producer
)

func SetupForMessageTests() {
	producerRepo := NewProducerRepository(testDB)
	producer, _ := data.NewProducer("producer1-for-message", successfulGetTestToken)
	producer.QuickFix()
	producer1, _ = producerRepo.Store(producer)
}

func getMessageRepository() MessageRepository {
	return NewMessageRepository(testDB, NewChannelRepository(testDB), NewProducerRepository(testDB))
}

func TestMessageGetByID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		repo := getMessageRepository()
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		assert.Nil(t, repo.Create(msg))
		rMsg, err := repo.GetByID(msg.ID.String())
		assert.Nil(t, err)
		assert.NotNil(t, rMsg)
		assert.Equal(t, channel1.ID, msg.BroadcastedTo.ID)
		assert.Equal(t, producer1.ID, msg.ProducedBy.ID)
		assert.Equal(t, samplePayload, msg.Payload)
		assert.Equal(t, sampleContentType, msg.ContentType)
	})
	t.Run("Fail", func(t *testing.T) {
		t.Parallel()
		repo := getMessageRepository()
		_, err := repo.GetByID("non-existing-id")
		assert.NotNil(t, err)
	})
}

func TestMessageGetCreate(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		repo := getMessageRepository()
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		_, err = repo.Get(channel1.ChannelID, msg.MessageID)
		assert.NotNil(t, err)
		assert.Nil(t, repo.Create(msg))
		var readMessage *data.Message
		readMessage, err = repo.Get(channel1.ChannelID, msg.MessageID)
		assert.Nil(t, err)
		assert.Equal(t, msg.MessageID, readMessage.MessageID)
		assert.Equal(t, msg.ID, readMessage.ID)
		assert.Equal(t, channel1.ChannelID, readMessage.BroadcastedTo.ChannelID)
		assert.Equal(t, producer1.ProducerID, readMessage.ProducedBy.ProducerID)
		assert.Equal(t, msg.ContentType, readMessage.ContentType)
		assert.Equal(t, msg.Payload, readMessage.Payload)
		assert.Equal(t, msg.Priority, readMessage.Priority)
		assert.Equal(t, msg.Status, readMessage.Status)
		assert.True(t, msg.ReceivedAt.Equal(readMessage.ReceivedAt))
		assert.True(t, msg.OutboxedAt.Equal(readMessage.OutboxedAt))
		assert.True(t, msg.CreatedAt.Equal(readMessage.CreatedAt))
		assert.True(t, msg.UpdatedAt.Equal(readMessage.UpdatedAt))
	})
	t.Run("InvalidMsgState", func(t *testing.T) {
		t.Parallel()
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		msg.MessageID = ""
		repo := getMessageRepository()
		assert.NotNil(t, repo.Create(msg))
	})
	t.Run("NonExistingChannel", func(t *testing.T) {
		t.Parallel()
		channel, _ := data.NewChannel("testchannel4msgtest", "token")
		channel.QuickFix()
		msg, err := data.NewMessage(channel, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		repo := getMessageRepository()
		err = repo.Create(msg)
		assert.NotNil(t, err)
		_, err = repo.Get(channel.ChannelID, msg.MessageID)
		assert.NotNil(t, err)
		assert.Equal(t, sql.ErrNoRows, err)
	})
	t.Run("NonExistingProducer", func(t *testing.T) {
		t.Parallel()
		producer, _ := data.NewProducer("testproducer4invalidprodinmsgtest", "testtoken")
		producer.QuickFix()
		msg, err := data.NewMessage(channel1, producer, samplePayload, sampleContentType)
		assert.Nil(t, err)
		repo := getMessageRepository()
		err = repo.Create(msg)
		assert.NotNil(t, err)
		_, err = repo.Get(channel1.ChannelID, msg.MessageID)
		assert.NotNil(t, err)
		assert.Equal(t, sql.ErrNoRows, err)
	})
	t.Run("DuplicateMessage", func(t *testing.T) {
		t.Parallel()
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		repo := getMessageRepository()
		assert.Nil(t, repo.Create(msg))
		err = repo.Create(msg)
		assert.NotNil(t, err)
		assert.Equal(t, ErrDuplicateMessageIDForChannel, err)
	})
	t.Run("ProducerReadErr", func(t *testing.T) {
		t.Parallel()
		expectedErr := errors.New("producer could not be read")
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		mockProducerRepository := new(MockProducerRepository)
		repo := NewMessageRepository(testDB, NewChannelRepository(testDB), mockProducerRepository)
		mockProducerRepository.On("Get", mock.Anything).Return(nil, expectedErr)
		assert.Nil(t, repo.Create(msg))
		_, err = repo.Get(channel1.ChannelID, msg.MessageID)
		assert.NotNil(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

func TestMessageSetDispatched(t *testing.T) {
	// Success tested along in TestDispatchMessage/Success
	t.Run("MessageNil", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		tx, _ := testDB.Begin()
		assert.Equal(t, ErrInvalidStateToSave, msgRepo.SetDispatched(context.WithValue(context.Background(), txContextKey, tx), nil))
	})
	t.Run("MessageInvalid", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		message := getMessageForJob()
		message.ReceivedAt = time.Time{}
		tx, _ := testDB.Begin()
		assert.Equal(t, ErrInvalidStateToSave, msgRepo.SetDispatched(context.WithValue(context.Background(), txContextKey, tx), message))
	})
	t.Run("NoTX", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		message := getMessageForJob()
		tx, _ := testDB.Begin()
		assert.Equal(t, ErrNoTxInContext, msgRepo.SetDispatched(context.WithValue(context.Background(), ContextKey("hello"), tx), message))
	})
}

func TestNormalizeMySQLError(t *testing.T) {
	assert.Equal(t, ErrDuplicateMessageIDForChannel, normalizeDBError(&mysql.MySQLError{Number: 1062}, mysqlErrorMap))
	assert.Nil(t, normalizeDBError(nil, mysqlErrorMap))
	assert.Equal(t, ErrDuplicateMessageIDForChannel, normalizeDBError(&sqlite.ErrConstraint, mysqlErrorMap))
	assert.Equal(t, ErrDuplicateMessageIDForChannel, normalizeDBError(&sqlite.ErrConstraintUnique, mysqlErrorMap))
}

func TestGetMessagesNotDispatchedForCertainPeriod(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		msg, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		msg.ReceivedAt = msg.ReceivedAt.Add(-5 * time.Second)
		msg2, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		err = msgRepo.Create(msg)
		assert.Nil(t, err)
		err = msgRepo.Create(msg2)
		assert.Nil(t, err)
		msgs := msgRepo.GetMessagesNotDispatchedForCertainPeriod(2 * time.Second)
		assert.Equal(t, 1, len(msgs))
		assert.Equal(t, msg.MessageID, msgs[0].MessageID)
	})
	t.Run("QueryError", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		oldLogger := log.Logger
		log.Logger = log.Output(&buf)
		defer func() { log.Logger = oldLogger }()
		errString := "sample select error"
		expectedErr := errors.New(errString)
		db, mock, _ := sqlmock.New()
		msgRepo := NewMessageRepository(db, NewChannelRepository(testDB), NewProducerRepository(testDB))
		mock.ExpectQuery(messageSelectRowCommonQuery).WillReturnError(expectedErr)
		mock.MatchExpectationsInOrder(true)
		msgs := msgRepo.GetMessagesNotDispatchedForCertainPeriod(2 * time.Second)
		assert.Equal(t, 0, len(msgs))
		assert.Nil(t, mock.ExpectationsWereMet())
		assert.Contains(t, buf.String(), errString)
	})
}

func TestGetMessagesByChannel(t *testing.T) {
	t.Run("PaginationDeadlock", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		_, _, err := msgRepo.GetMessagesForChannel(channel2.ChannelID, data.NewPagination(channel1, channel2))
		assert.NotNil(t, err)
		assert.Equal(t, ErrPaginationDeadlock, err)
	})
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		msg, err := data.NewMessage(channel2, producer1, samplePayload, sampleContentType)
		assert.Nil(t, err)
		msg2, err := data.NewMessage(channel1, producer1, samplePayload, sampleContentType)
		err = msgRepo.Create(msg)
		assert.Nil(t, err)
		err = msgRepo.Create(msg2)
		assert.Nil(t, err)
		msgs, page, err := msgRepo.GetMessagesForChannel(channel2.ChannelID, data.NewPagination(nil, nil))
		assert.Nil(t, err)
		assert.NotNil(t, page)
		assert.NotNil(t, page.Next)
		assert.NotNil(t, page.Previous)
		assert.Equal(t, 1, len(msgs))
		assert.Equal(t, msg.ID, msgs[0].ID)
		msgs, page3, err := msgRepo.GetMessagesForChannel(channel2.ChannelID, &data.Pagination{Previous: page.Previous})
		assert.Nil(t, err)
		assert.NotNil(t, page3)
		assert.Nil(t, page3.Next)
		assert.Nil(t, page3.Previous)
		assert.Equal(t, 0, len(msgs))
		msgs, page2, err := msgRepo.GetMessagesForChannel(channel2.ChannelID, &data.Pagination{Next: page.Next})
		assert.Nil(t, err)
		assert.NotNil(t, page2)
		assert.Nil(t, page2.Next)
		assert.Nil(t, page2.Previous)
		assert.Equal(t, 0, len(msgs))
	})
	t.Run("NonExistingChannel", func(t *testing.T) {
		t.Parallel()
		msgRepo := getMessageRepository()
		_, _, err := msgRepo.GetMessagesForChannel(channel2.ChannelID+"NONE", data.NewPagination(nil, nil))
		assert.Equal(t, sql.ErrNoRows, err)
	})
}
