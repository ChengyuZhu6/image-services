package server

import (
	"context"
	"time"

	"cri-image-service/pkg/service"

	"google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type ImageServer struct {
	runtime.UnimplementedImageServiceServer
	imageService *service.ImageService
}

func NewImageServer() *ImageServer {
	return &ImageServer{
		imageService: service.NewImageService(),
	}
}

// PullImage implements image pulling
func (s *ImageServer) PullImage(ctx context.Context, req *runtime.PullImageRequest) (*runtime.PullImageResponse, error) {
	if req.GetImage() == nil {
		return nil, status.Error(codes.InvalidArgument, "image config is nil")
	}

	imageRef := req.GetImage().GetImage()
	if imageRef == "" {
		return nil, status.Error(codes.InvalidArgument, "image reference is empty")
	}

	imageID, err := s.imageService.PullImage(ctx, imageRef, req.GetAuth())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to pull image: %v", err)
	}

	return &runtime.PullImageResponse{
		ImageRef: imageID,
	}, nil
}

// RemoveImage implements image removal
func (s *ImageServer) RemoveImage(ctx context.Context, req *runtime.RemoveImageRequest) (*runtime.RemoveImageResponse, error) {
	if req.GetImage() == nil {
		return nil, status.Error(codes.InvalidArgument, "image is nil")
	}

	err := s.imageService.RemoveImage(ctx, req.GetImage().GetImage())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to remove image: %v", err)
	}

	return &runtime.RemoveImageResponse{}, nil
}

// ImageStatus implements image status retrieval
func (s *ImageServer) ImageStatus(ctx context.Context, req *runtime.ImageStatusRequest) (*runtime.ImageStatusResponse, error) {
	if req.GetImage() == nil {
		return nil, status.Error(codes.InvalidArgument, "image is nil")
	}

	imgStatus, err := s.imageService.ImageStatus(ctx, req.GetImage().GetImage())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get image status: %v", err)
	}

	return &runtime.ImageStatusResponse{
		Image: imgStatus,
	}, nil
}

// ListImages implements listing all images
func (s *ImageServer) ListImages(ctx context.Context, req *runtime.ListImagesRequest) (*runtime.ListImagesResponse, error) {
	images, err := s.imageService.ListImages(ctx, req.GetFilter())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list images: %v", err)
	}

	return &runtime.ListImagesResponse{
		Images: images,
	}, nil
}

// ImageFsInfo implements retrieving filesystem information
func (s *ImageServer) ImageFsInfo(ctx context.Context, req *runtime.ImageFsInfoRequest) (*runtime.ImageFsInfoResponse, error) {
	// Return basic information about image storage
	return &runtime.ImageFsInfoResponse{
		ImageFilesystems: []*runtime.FilesystemUsage{
			{
				Timestamp: time.Now().UnixNano(),
				FsId: &runtime.FilesystemIdentifier{
					Mountpoint: s.imageService.GetImageRoot(),
				},
				UsedBytes:  &runtime.UInt64Value{Value: 0},
				InodesUsed: &runtime.UInt64Value{Value: 0},
			},
		},
	}, nil
}
