package service

import (
	"context"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeffinity/oculus/app/mh/internal/biz"
	mhv1 "github.com/jeffinity/oculus/proto/mh/v1"
)

type ComicService struct {
	mhv1.UnimplementedMHComicServiceServer

	uc  *biz.ComicUseCase
	log *log.Helper
}

func NewComicService(uc *biz.ComicUseCase, logger log.Logger) *ComicService {
	return &ComicService{uc: uc, log: log.NewHelper(log.With(logger, "module", "mh/ComicService"))}
}

func (s *ComicService) ListBooks(ctx context.Context, req *mhv1.ListBooksRequest) (*mhv1.ListBooksReply, error) {
	if req == nil {
		req = &mhv1.ListBooksRequest{}
	}
	page := int(req.GetPage())
	if page <= 0 {
		page = 1
	}
	size := int(req.GetSize())
	if size <= 0 {
		size = 20
	}

	items, total, totalPages, err := s.uc.ListBooks(ctx, page, size)
	if err != nil {
		s.log.Errorf("list books failed: %v", err)
		return nil, status.Error(codes.Internal, "list books failed")
	}

	ret := &mhv1.ListBooksReply{
		Page:       int32(page),
		Size:       int32(size),
		Total:      total,
		TotalPages: int32(totalPages),
		Items:      make([]*mhv1.BookListItem, 0, len(items)),
	}
	for i := range items {
		ret.Items = append(ret.Items, &mhv1.BookListItem{
			Title:      safeString(items[i].Title),
			OrgId:      items[i].OrgID,
			Desc:       safeString(items[i].Desc),
			CoverUrl:   safeString(items[i].CoverURL),
			UpdateTime: safeString(items[i].UpdateTime),
			Cover:      safeStringMap(items[i].Cover),
		})
	}
	return ret, nil
}

func (s *ComicService) ListLatest(ctx context.Context, req *mhv1.ListLatestRequest) (*mhv1.ListLatestReply, error) {
	if req == nil {
		req = &mhv1.ListLatestRequest{}
	}
	page := int(req.GetPage())
	if page <= 0 {
		page = 1
	}
	size := int(req.GetSize())
	if size <= 0 {
		size = 20
	}

	items, total, totalPages, err := s.uc.ListLatest(ctx, page, size)
	if err != nil {
		s.log.Errorf("list latest failed: %v", err)
		return nil, status.Error(codes.Internal, "list latest failed")
	}

	ret := &mhv1.ListLatestReply{
		Page:       int32(page),
		Size:       int32(size),
		Total:      total,
		TotalPages: int32(totalPages),
		Items:      make([]*mhv1.LatestListItem, 0, len(items)),
	}
	for i := range items {
		ret.Items = append(ret.Items, &mhv1.LatestListItem{
			Cname:      safeString(items[i].CName),
			Title:      safeString(items[i].Title),
			OrgId:      items[i].OrgID,
			Cid:        items[i].CID,
			Prefix:     safeString(items[i].Prefix),
			CoverUrl:   safeString(items[i].CoverURL),
			UpdateTime: safeString(items[i].UpdateTime),
		})
	}
	return ret, nil
}

func (s *ComicService) GetBook(ctx context.Context, req *mhv1.GetBookRequest) (*mhv1.GetBookReply, error) {
	if req == nil || req.GetOrgId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "org_id is required")
	}

	book, chapters, lastCID, err := s.uc.GetBookDetail(ctx, req.GetOrgId())
	if err != nil {
		s.log.Errorf("get book failed: org_id=%d err=%v", req.GetOrgId(), err)
		return nil, status.Error(codes.Internal, "get book failed")
	}
	if book == nil {
		return nil, status.Error(codes.NotFound, "book not found")
	}

	ret := &mhv1.GetBookReply{
		Book: &mhv1.BookListItem{
			Title:      safeString(book.Title),
			OrgId:      book.OrgID,
			Desc:       safeString(book.Desc),
			CoverUrl:   safeString(book.CoverURL),
			UpdateTime: safeString(book.UpdateTime),
			Cover:      safeStringMap(book.Cover),
		},
		Chapters: make([]*mhv1.ChapterMeta, 0, len(chapters)),
		LastCid:  lastCID,
	}
	for i := range chapters {
		ret.Chapters = append(ret.Chapters, &mhv1.ChapterMeta{
			Cid:    chapters[i].CID,
			Name:   safeString(chapters[i].Name),
			Prefix: safeString(chapters[i].Prefix),
		})
	}
	return ret, nil
}

func (s *ComicService) GetChapter(ctx context.Context, req *mhv1.GetChapterRequest) (*mhv1.GetChapterReply, error) {
	if req == nil || req.GetOrgId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "org_id is required")
	}

	page, err := s.uc.GetChapterPage(ctx, req.GetOrgId(), req.GetCid(), int(req.GetIndex()), req.GetPrefix())
	if err != nil {
		s.log.Errorf("get chapter failed: org_id=%d cid=%d index=%d err=%v", req.GetOrgId(), req.GetCid(), req.GetIndex(), err)
		return nil, status.Error(codes.Internal, "get chapter failed")
	}

	return &mhv1.GetChapterReply{
		ChapterName: safeString(page.ChapterName),
		Cid:         page.CID,
		Index:       int32(page.Index),
		Page:        int32(page.Page),
		TotalPages:  int32(page.TotalPages),
		PrevCid:     page.PrevCID,
		NextCid:     page.NextCID,
		Prefix:      safeString(page.Prefix),
		Images:      safeStringSlice(page.Images),
	}, nil
}

func (s *ComicService) Sync(ctx context.Context, _ *mhv1.SyncRequest) (*mhv1.OperationReply, error) {
	if err := s.uc.Sync(ctx); err != nil {
		s.log.Errorf("sync failed: %v", err)
		return nil, status.Error(codes.Internal, "sync failed")
	}
	return &mhv1.OperationReply{Message: "ok"}, nil
}

func (s *ComicService) Proot(ctx context.Context, _ *mhv1.ProotRequest) (*mhv1.OperationReply, error) {
	if err := s.uc.Proof(ctx); err != nil {
		s.log.Errorf("proot failed: %v", err)
		return nil, status.Error(codes.Internal, "proot failed")
	}
	return &mhv1.OperationReply{Message: "ok"}, nil
}

func safeString(s string) string {
	return strings.ToValidUTF8(s, "")
}

func safeStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[safeString(k)] = safeString(v)
	}
	return out
}

func safeStringSlice(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	for i := range in {
		out = append(out, safeString(in[i]))
	}
	return out
}
