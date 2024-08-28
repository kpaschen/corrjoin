package comparisons

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	messages "github.com/kpaschen/corrjoin/lib/kafka"
	"github.com/kpaschen/corrjoin/lib/settings"
	kafka "github.com/segmentio/kafka-go"
	"log"
)

const (
	KAFKA_BUFFER_SIZE = 100
)

// A KafkaComparer sends lists of pairs as kafka messages for
// consumers to pick up and process. It then listens for the
// results.
type KafkaComparer struct {
	config           settings.CorrjoinSettings
	normalizedMatrix [][]float64
	constantRows     []bool
	strideCounter    int
	resultChannel    chan<- *datatypes.CorrjoinResult
	candidateBuffer  []datatypes.RowPair

	pairWriter        *kafka.Writer
	correlationReader *kafka.Reader
	runnerCtx         context.Context
	runnerCancel      context.CancelFunc
}

func (k *KafkaComparer) Initialize(config settings.CorrjoinSettings, results chan<- *datatypes.CorrjoinResult) {
	k.config = config
	k.resultChannel = results
	k.strideCounter = -1
	k.candidateBuffer = make([]datatypes.RowPair, 0, KAFKA_BUFFER_SIZE)
	k.pairWriter = &kafka.Writer{
		Addr:     kafka.TCP(config.KafkaURL),
		Topic:    "corrjoin_pairs",
		Balancer: &kafka.LeastBytes{},
	}
	k.correlationReader = kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{config.KafkaURL},
		GroupID: "2",
		Topic:   "corrjoin_results",
	})

	k.runnerCtx, k.runnerCancel = context.WithCancel(context.Background())
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				log.Printf("runner stopped\n")
				return
			default:
				msg, err := k.correlationReader.ReadMessage(ctx)
				if err != nil {
					log.Printf("error getting correlation message: %v\n", err)
					continue
				}
				correlation := &datatypes.CorrjoinResult{}
				err = k.decodeCorrelationMessage(msg, correlation)
				if err != nil {
					log.Printf("error decoding correlation message: %v\n", err)
					continue
				}
				log.Printf("send correlation result to channel\n")
				k.resultChannel <- correlation

			}
		}
	}(k.runnerCtx)
	log.Printf("kafka comparer initialized with url %s\n", config.KafkaURL)
}

func (k *KafkaComparer) StartStride(normalizedMatrix [][]float64, constantRows []bool, strideCounter int) error {
	if strideCounter < k.strideCounter {
		return fmt.Errorf("got new stride %d but current stride %d is larger",
			strideCounter, k.strideCounter)
	}
	if strideCounter == k.strideCounter {
		return fmt.Errorf("repeated StartStride call for stride %d", strideCounter)
	}
	k.strideCounter = strideCounter
	k.normalizedMatrix = normalizedMatrix
	k.constantRows = constantRows
	return nil
}

func (k *KafkaComparer) Compare(index1 int, index2 int) error {
	if IsConstantRow(index1, k.constantRows) || IsConstantRow(index2, k.constantRows) {
		return nil
	}
	k.candidateBuffer = append(k.candidateBuffer, *datatypes.NewRowPair(index1, index2))
	if len(k.candidateBuffer) >= KAFKA_BUFFER_SIZE {
		key := fmt.Sprintf("key-%d", k.strideCounter)
		msgBytes, err := k.encodePairMessage()
		if err != nil {
			return err
		}
		msg := kafka.Message{
			Key:   []byte(key),
			Value: msgBytes,
		}
		err = k.pairWriter.WriteMessages(context.Background(), msg)
		if err != nil {
			log.Printf("error sending message: %v\n", err)
		}
		k.candidateBuffer = make([]datatypes.RowPair, 0, KAFKA_BUFFER_SIZE)
	}
	return nil
}

func (k *KafkaComparer) StopStride(strideCounter int) error {
	if strideCounter != k.strideCounter {
		return fmt.Errorf("trying to stop stride %d but i am processing stride %d",
			strideCounter, k.strideCounter)
	}
	// TODO: this function is called when no further comparison requests will be arriving
	// for the stride.
	// We may still be waiting for kafka workers to return values, so should hold off sending
	// the stride end to the results channel.
	k.resultChannel <- &datatypes.CorrjoinResult{
		CorrelatedPairs: map[datatypes.RowPair]float64{},
		StrideCounter:   strideCounter,
	}
	log.Printf("stride %d complete\n", strideCounter)
	return nil
}

func (k *KafkaComparer) Shutdown() error {
	log.Println("Kafka Comparer shutting down")
	k.resultChannel <- &datatypes.CorrjoinResult{
		CorrelatedPairs: map[datatypes.RowPair]float64{},
		StrideCounter:   k.strideCounter,
	}
	if k.pairWriter != nil {
		k.pairWriter.Close()
	}
	if k.correlationReader != nil {
		k.correlationReader.Close()
	}
	if k.runnerCancel != nil {
		k.runnerCancel()
	}
	return nil
}

func (k *KafkaComparer) encodePairMessage() ([]byte, error) {
	rows := make(map[int][]float64)
	for _, p := range k.candidateBuffer {
		ids := p.RowIds()
		_, have := rows[ids[0]]
		if !have {
			rows[ids[0]] = k.normalizedMatrix[ids[0]]
		}
		_, have = rows[ids[1]]
		if !have {
			rows[ids[1]] = k.normalizedMatrix[ids[1]]
		}
	}
	return json.Marshal(messages.PairMessage{
		StrideCounter: k.strideCounter,
		Config:        k.config,
		Pairs:         k.candidateBuffer,
		Rows:          rows,
	})
}

func (k *KafkaComparer) decodeCorrelationMessage(msg kafka.Message, res *datatypes.CorrjoinResult) error {
	return json.Unmarshal(msg.Value, res)
}
