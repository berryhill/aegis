package graph

import "errors"

// ProducerConsumerPlan resolves only a two-node, single-artifact chain. It is
// immutable definition/input data, not a scheduler, execution grant or evidence
// that either node has run. Published Graph snapshots retain their existing v1
// codec; direct Loop execution envelopes keep their single-node meaning.
//
// Values returned here are independent copies of the sealed inputs. The owning
// controller must resolve exact active Loop interfaces and repeat admission at
// every effect; Graph port assertions alone are not executable Loop contracts.
type ProducerConsumerPlan struct {
	Producer       Node
	Consumer       Node
	Edge           Dependency
	ProducerInputs []NormalizedInput
	ConsumerInputs []NormalizedInput
}

// ResolveProducerConsumerPlan requires owning-service published readback. The
// one supported order comes from the exact edge, never node slice order. Both
// nodes use one exact participant: this does not union authority across Agents.
// The consumer's artifact port is intentionally absent from ConsumerInputs;
// only a separately verified, lineage-bound artifact may fill it.
func ResolveProducerConsumerPlan(snapshot GraphRunSnapshot, published GraphRevision) (ProducerConsumerPlan, error) {
	topology, err := ResolveRunTopology(snapshot, &published, nil)
	if err != nil {
		return ProducerConsumerPlan{}, err
	}
	denied := errors.New("exact two-node single-artifact producer-consumer Graph required")
	if snapshot.SchemaVersion != SnapshotSchemaVersion || len(topology.Nodes) != 2 || len(topology.Dependencies) != 1 || len(topology.Dependencies[0].Mappings) != 1 {
		return ProducerConsumerPlan{}, denied
	}
	edge := topology.Dependencies[0]
	var producer, consumer Node
	for _, node := range topology.Nodes {
		if node.ID == edge.FromNodeID {
			producer = node
		}
		if node.ID == edge.ToNodeID {
			consumer = node
		}
	}
	if producer.ID == "" || consumer.ID == "" || producer.Participant != consumer.Participant {
		return ProducerConsumerPlan{}, denied
	}
	mapping := edge.Mappings[0]
	output, ok := portByID(producer.Outputs, mapping.FromPort)
	if !ok || output.Type != TypeArtifact || !output.Required || len(producer.Outputs) != 1 {
		return ProducerConsumerPlan{}, denied
	}
	input, ok := portByID(consumer.Inputs, mapping.ToPort)
	if !ok || input.Type != TypeArtifact || !input.Required {
		return ProducerConsumerPlan{}, denied
	}
	// Every exposed Graph result comes from the consumer. Producer evidence is
	// not aggregate completion, even when its content digest is correct.
	if len(topology.Outputs) == 0 || len(topology.OutputMappings) == 0 {
		return ProducerConsumerPlan{}, denied
	}
	for _, mapping := range topology.OutputMappings {
		if mapping.FromNodeID != consumer.ID {
			return ProducerConsumerPlan{}, denied
		}
	}
	for _, port := range topology.Inputs {
		if port.Type == TypeArtifact {
			return ProducerConsumerPlan{}, denied
		}
	}
	values := make(map[string]NormalizedInput, len(snapshot.Inputs))
	for _, value := range snapshot.Inputs {
		values[value.PortID] = value
	}
	result := ProducerConsumerPlan{Producer: producer, Consumer: consumer, Edge: edge, ProducerInputs: []NormalizedInput{}, ConsumerInputs: []NormalizedInput{}}
	for _, mapping := range topology.InputMappings {
		value, found := values[mapping.GraphInput]
		if !found {
			continue
		} // structurally valid absent optional Graph input
		value.PortID = mapping.ToPort
		value.Value = append([]byte(nil), value.Value...)
		switch mapping.ToNodeID {
		case producer.ID:
			result.ProducerInputs = append(result.ProducerInputs, value)
		case consumer.ID:
			result.ConsumerInputs = append(result.ConsumerInputs, value)
		default:
			return ProducerConsumerPlan{}, denied
		}
	}
	return result, nil
}
