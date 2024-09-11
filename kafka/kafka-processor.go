package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	messages "github.com/kpaschen/corrjoin/lib/kafka"
	kafka "github.com/segmentio/kafka-go"
	"log"
)

func decodePairMessage(msg kafka.Message, pairs *messages.PairMessage) error {
	return json.Unmarshal(msg.Value, pairs)
}

func encodeCorrelationMessage(corr datatypes.CorrjoinResult) ([]byte, error) {
	return (&corr).MarshalJSON()
}

func main() {
	var kafkaURL string
	flag.StringVar(&kafkaURL, "kafkaURL", "", "The URL for the kafka broker.")
	flag.Parse()

	kafkaPairReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaURL},
		// TODO: add partition here so we can have multiple pair readers
		GroupID: "1",
		Topic:   "corrjoin_pairs",
	})
	defer kafkaPairReader.Close()

	kafkaResultsWriter := &kafka.Writer{
		Addr:  kafka.TCP(kafkaURL),
		Topic: "corrjoin_results",
		// Balancer: &kafka.LeastBytes{},
		Balancer: &kafka.Hash{},
	}
	defer kafkaResultsWriter.Close()

	log.Println("Kafka worker waiting for pairs to compare")
	for {
		msg, err := kafkaPairReader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("failed to open pair reader: %v\n", err)
			continue
		}
		// parse and process
		pairs := &messages.PairMessage{}
		log.Printf("received pairs message with key %s, partition %d\n", string(msg.Key[:]), msg.Partition)
		err = decodePairMessage(msg, pairs)
		if err != nil {
			log.Printf("failed to decode pair message: %v\n", err)
			continue
		}

		comparer := *comparisons.NewBaseComparer(
			pairs.Config,
			pairs.StrideCounter,
			pairs.Rows,
		)
		result := datatypes.CorrjoinResult{
			StrideCounter:   pairs.StrideCounter,
			CorrelatedPairs: make(map[datatypes.RowPair]float64),
		}
		for _, p := range pairs.Pairs {
			ids := p.RowIds()
			pearson, err := comparer.Compare(ids[0], ids[1])
			if err != nil {
				log.Printf("error comparing %d and %d: %v\n", ids[0], ids[1], err)
				continue
			}
			if pearson > 0.0 {
				log.Printf("got a match: %d and %d correlated at %f\n", ids[0], ids[1], pearson)
				result.CorrelatedPairs[p] = pearson
			}
		}
		if len(result.CorrelatedPairs) > 0 {
			key := fmt.Sprintf("corr-%d", pairs.StrideCounter)
			valueBytes, err := encodeCorrelationMessage(result)
			if err != nil {
				log.Printf("error encoding correlation message: %v\n", err)
				continue
			}
			msg := kafka.Message{
				Key:   []byte(key),
				Value: valueBytes,
			}
			err = kafkaResultsWriter.WriteMessages(context.Background(), msg)
			if err != nil {
				log.Printf("failed to send kafka correlation message: %v\n", err)
			} else {
				log.Printf("sent %d correlated pairs back\n", len(result.CorrelatedPairs))
			}
		}
	}
}
