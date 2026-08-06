package vectorstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/config"
	pb "github.com/qdrant/go-client/qdrant"
)

type qdrantStore struct {
	client *pb.Client
	config config.VectorStoreConfig
}

// NewQdrantStore 创建 Qdrant 向量存储
func NewQdrantStore(cfg config.VectorStoreConfig) (VectorStore, error) {
	host, port := parseHostPort(cfg.Host)

	client, err := pb.NewClient(&pb.Config{
		Host: host,
		Port: port,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 Qdrant 失败: %w", err)
	}

	return &qdrantStore{
		client: client,
		config: cfg,
	}, nil
}

func (s *qdrantStore) EnsureCollection(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, s.config.CollectionName)
	if err != nil {
		return fmt.Errorf("检查 Collection 失败: %w", err)
	}
	if exists {
		return nil
	}

	distance := pb.Distance_Cosine
	switch strings.ToLower(s.config.Distance) {
	case "euclid":
		distance = pb.Distance_Euclid
	case "dot":
		distance = pb.Distance_Dot
	}

	err = s.client.CreateCollection(ctx, &pb.CreateCollection{
		CollectionName: s.config.CollectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     uint64(s.config.Dimension),
			Distance: distance,
		}),
	})
	if err != nil {
		return fmt.Errorf("创建 Collection 失败: %w", err)
	}

	return nil
}

func (s *qdrantStore) Upsert(ctx context.Context, records []VectorRecord) error {
	if len(records) == 0 {
		return nil
	}

	points := make([]*pb.PointStruct, 0, len(records))
	for _, r := range records {
		payload := make(map[string]*pb.Value)
		for k, v := range r.Payload {
			payload[k] = toQdrantValue(v)
		}

		points = append(points, &pb.PointStruct{
			Id:      pb.NewID(r.ID),
			Vectors: pb.NewVectorsDense(r.Vector),
			Payload: payload,
		})
	}

	waitTrue := true
	_, err := s.client.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: s.config.CollectionName,
		Points:         points,
		Wait:           &waitTrue,
	})
	if err != nil {
		return fmt.Errorf("Upsert 失败: %w", err)
	}

	return nil
}

func (s *qdrantStore) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	topK := uint64(req.TopK)
	if topK == 0 {
		topK = 10
	}

	queryReq := &pb.QueryPoints{
		CollectionName: s.config.CollectionName,
		Query:          pb.NewQueryDense(req.Vector),
		Limit:          &topK,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	}

	if len(req.Filter) > 0 {
		queryReq.Filter = buildFilter(req.Filter)
	}

	scored, err := s.client.Query(ctx, queryReq)
	if err != nil {
		return nil, fmt.Errorf("Search 失败: %w", err)
	}

	results := make([]SearchResult, 0, len(scored))
	for _, sp := range scored {
		payload := make(map[string]any)
		for k, v := range sp.Payload {
			payload[k] = fromQdrantValue(v)
		}

		id := ""
		if sp.Id != nil {
			if uuid, ok := sp.Id.PointIdOptions.(*pb.PointId_Uuid); ok {
				id = uuid.Uuid
			}
		}

		results = append(results, SearchResult{
			ID:      id,
			Score:   sp.Score,
			Payload: payload,
		})
	}

	return results, nil
}

func (s *qdrantStore) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	pointIds := make([]*pb.PointId, 0, len(ids))
	for _, id := range ids {
		pointIds = append(pointIds, pb.NewID(id))
	}

	waitTrue := true
	_, err := s.client.Delete(ctx, &pb.DeletePoints{
		CollectionName: s.config.CollectionName,
		Points:         pb.NewPointsSelector(pointIds...),
		Wait:           &waitTrue,
	})
	if err != nil {
		return fmt.Errorf("Delete 失败: %w", err)
	}

	return nil
}

// Get 按 ID 取单个点 payload（引用来源查看 chunk 原文用）
func (s *qdrantStore) Get(ctx context.Context, id string) (map[string]any, bool, error) {
	points, err := s.client.Get(ctx, &pb.GetPoints{
		CollectionName: s.config.CollectionName,
		Ids:            []*pb.PointId{pb.NewID(id)},
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, false, fmt.Errorf("Get 失败: %w", err)
	}
	if len(points) == 0 {
		return nil, false, nil
	}
	payload := make(map[string]any, len(points[0].Payload))
	for k, v := range points[0].Payload {
		payload[k] = fromQdrantValue(v)
	}
	return payload, true, nil
}

func (s *qdrantStore) Close() error {
	return s.client.Close()
}

func buildFilter(filter map[string]any) *pb.Filter {
	var conditions []*pb.Condition
	for key, val := range filter {
		switch v := val.(type) {
		case string:
			conditions = append(conditions, pb.NewMatchKeyword(key, v))
		case int:
			conditions = append(conditions, pb.NewMatchInt(key, int64(v)))
		case int64:
			conditions = append(conditions, pb.NewMatchInt(key, v))
		}
	}
	if len(conditions) == 0 {
		return nil
	}
	return &pb.Filter{Must: conditions}
}

func toQdrantValue(v any) *pb.Value {
	switch val := v.(type) {
	case string:
		return &pb.Value{Kind: &pb.Value_StringValue{StringValue: val}}
	case int:
		return &pb.Value{Kind: &pb.Value_IntegerValue{IntegerValue: int64(val)}}
	case int64:
		return &pb.Value{Kind: &pb.Value_IntegerValue{IntegerValue: val}}
	case float64:
		return &pb.Value{Kind: &pb.Value_DoubleValue{DoubleValue: val}}
	case bool:
		return &pb.Value{Kind: &pb.Value_BoolValue{BoolValue: val}}
	default:
		return &pb.Value{Kind: &pb.Value_StringValue{StringValue: fmt.Sprintf("%v", val)}}
	}
}

func fromQdrantValue(v *pb.Value) any {
	if v == nil {
		return nil
	}
	switch k := v.Kind.(type) {
	case *pb.Value_StringValue:
		return k.StringValue
	case *pb.Value_IntegerValue:
		return k.IntegerValue
	case *pb.Value_DoubleValue:
		return k.DoubleValue
	case *pb.Value_BoolValue:
		return k.BoolValue
	default:
		return nil
	}
}

func parseHostPort(host string) (string, int) {
	parts := strings.Split(host, ":")
	if len(parts) == 2 {
		port := 6334
		fmt.Sscanf(parts[1], "%d", &port)
		return parts[0], port
	}
	return host, 6334
}
