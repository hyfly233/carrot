package localizer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ResourceLocalizer 资源本地化器
type ResourceLocalizer struct {
	localizer *Localizer
	logger    *zap.Logger
}

// LocalizationRequest 扩展本地化请求，包含特定的处理逻辑
type ResourceLocalizationRequest struct {
	*LocalizationRequest
	UserName    string
	AppID       string
	Permissions os.FileMode
}

// NewResourceLocalizer 创建资源本地化器
func NewResourceLocalizer(localizer *Localizer) *ResourceLocalizer {
	return &ResourceLocalizer{
		localizer: localizer,
		logger:    common.ComponentLogger("resource-localizer"),
	}
}

// LocalizeContainerResources 本地化容器资源
func (rl *ResourceLocalizer) LocalizeContainerResources(
	containerID common.ContainerID,
	launchContext *common.ContainerLaunchContext,
	callback LocalizationCallback) (string, error) {

	// 解析启动上下文中的资源
	resources, err := rl.parseResources(launchContext)
	if err != nil {
		return "", fmt.Errorf("failed to parse resources: %v", err)
	}

	if len(resources) == 0 {
		rl.logger.Debug("No resources to localize",
			zap.String("container_id", rl.getContainerKey(containerID)))
		// 即使没有资源也调用回调表示完成
		if callback != nil {
			callback.OnCompleted("", []*LocalResource{})
		}
		return "", nil
	}

	rl.logger.Info("Localizing container resources",
		zap.String("container_id", rl.getContainerKey(containerID)),
		zap.Int("resource_count", len(resources)))

	// 创建包装回调以处理后处理逻辑
	wrappedCallback := &ResourceLocalizationCallback{
		originalCallback:  callback,
		resourceLocalizer: rl,
		containerID:       containerID,
		logger:            rl.logger,
	}

	return rl.localizer.LocalizeResources(containerID, resources, wrappedCallback)
}

// parseResources 解析资源
func (rl *ResourceLocalizer) parseResources(launchContext *common.ContainerLaunchContext) ([]*LocalResource, error) {
	var resources []*LocalResource

	// 解析本地资源
	for _, localRes := range launchContext.LocalResources {
		resource := &LocalResource{
			URL:        localRes.Resource,
			Type:       rl.convertResourceType(localRes.Type),
			Size:       localRes.Size,
			Visibility: rl.convertVisibility(localRes.Visibility),
		}

		resources = append(resources, resource)
	}

	// 解析服务数据（如果有）
	if len(launchContext.ServiceData) > 0 {
		for name, data := range launchContext.ServiceData {
			// 创建临时文件用于服务数据
			resource := &LocalResource{
				URL:        fmt.Sprintf("data://%s", name),
				Type:       ResourceTypeFile,
				Size:       int64(len(data)),
				Visibility: ResourceVisibilityPrivate,
			}
			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// convertResourceType 转换资源类型
func (rl *ResourceLocalizer) convertResourceType(resourceType string) ResourceType {
	switch strings.ToUpper(resourceType) {
	case "FILE":
		return ResourceTypeFile
	case "ARCHIVE":
		return ResourceTypeArchive
	case "PATTERN":
		return ResourceTypePattern
	default:
		return ResourceTypeFile
	}
}

// convertVisibility 转换可见性
func (rl *ResourceLocalizer) convertVisibility(visibility string) ResourceVisibility {
	switch strings.ToUpper(visibility) {
	case "PRIVATE":
		return ResourceVisibilityPrivate
	case "APPLICATION":
		return ResourceVisibilityApplication
	case "PUBLIC":
		return ResourceVisibilityPublic
	default:
		return ResourceVisibilityPrivate
	}
}

// getContainerKey 获取容器键
func (rl *ResourceLocalizer) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID)
}

// ResourceLocalizationCallback 资源本地化回调
type ResourceLocalizationCallback struct {
	originalCallback  LocalizationCallback
	resourceLocalizer *ResourceLocalizer
	containerID       common.ContainerID
	logger            *zap.Logger
}

// OnProgress 进度回调
func (rlc *ResourceLocalizationCallback) OnProgress(requestID string, progress float64) {
	if rlc.originalCallback != nil {
		rlc.originalCallback.OnProgress(requestID, progress)
	}
}

// OnCompleted 完成回调
func (rlc *ResourceLocalizationCallback) OnCompleted(requestID string, resources []*LocalResource) {
	rlc.logger.Info("Resource localization completed, starting post-processing",
		zap.String("request_id", requestID),
		zap.Int("resource_count", len(resources)))

	// 执行后处理
	processedResources, err := rlc.postProcessResources(resources)
	if err != nil {
		rlc.logger.Error("Failed to post-process resources",
			zap.String("request_id", requestID),
			zap.Error(err))
		if rlc.originalCallback != nil {
			rlc.originalCallback.OnFailed(requestID, err)
		}
		return
	}

	rlc.logger.Info("Resource post-processing completed",
		zap.String("request_id", requestID))

	if rlc.originalCallback != nil {
		rlc.originalCallback.OnCompleted(requestID, processedResources)
	}
}

// OnFailed 失败回调
func (rlc *ResourceLocalizationCallback) OnFailed(requestID string, err error) {
	if rlc.originalCallback != nil {
		rlc.originalCallback.OnFailed(requestID, err)
	}
}

// postProcessResources 后处理资源
func (rlc *ResourceLocalizationCallback) postProcessResources(resources []*LocalResource) ([]*LocalResource, error) {
	var processedResources []*LocalResource

	for _, resource := range resources {
		switch resource.Type {
		case ResourceTypeArchive:
			// 解压归档文件
			extractedResources, err := rlc.extractArchive(resource)
			if err != nil {
				return nil, fmt.Errorf("failed to extract archive %s: %v", resource.LocalPath, err)
			}
			processedResources = append(processedResources, extractedResources...)

		case ResourceTypePattern:
			// 处理模式匹配
			matchedResources, err := rlc.processPattern(resource)
			if err != nil {
				return nil, fmt.Errorf("failed to process pattern %s: %v", resource.LocalPath, err)
			}
			processedResources = append(processedResources, matchedResources...)

		default:
			// 普通文件，设置权限
			if err := rlc.setFilePermissions(resource); err != nil {
				rlc.logger.Warn("Failed to set file permissions",
					zap.String("file", resource.LocalPath),
					zap.Error(err))
			}
			processedResources = append(processedResources, resource)
		}
	}

	return processedResources, nil
}

// extractArchive 解压归档文件
func (rlc *ResourceLocalizationCallback) extractArchive(resource *LocalResource) ([]*LocalResource, error) {
	rlc.logger.Debug("Extracting archive", zap.String("file", resource.LocalPath))

	// 创建解压目录
	extractDir := resource.LocalPath + "_extracted"
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, err
	}

	// 根据文件扩展名决定解压方式
	fileName := filepath.Base(resource.LocalPath)
	var err error

	if strings.HasSuffix(fileName, ".tar.gz") || strings.HasSuffix(fileName, ".tgz") {
		err = rlc.extractTarGz(resource.LocalPath, extractDir)
	} else if strings.HasSuffix(fileName, ".tar") {
		err = rlc.extractTar(resource.LocalPath, extractDir)
	} else if strings.HasSuffix(fileName, ".zip") {
		err = rlc.extractZip(resource.LocalPath, extractDir)
	} else {
		return nil, fmt.Errorf("unsupported archive format: %s", fileName)
	}

	if err != nil {
		return nil, err
	}

	// 收集解压后的文件
	var extractedResources []*LocalResource
	err = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			extractedResource := &LocalResource{
				URL:        "extracted://" + path,
				LocalPath:  path,
				Type:       ResourceTypeFile,
				Size:       info.Size(),
				Visibility: resource.Visibility,
				Downloaded: true,
			}
			extractedResources = append(extractedResources, extractedResource)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	rlc.logger.Debug("Archive extracted",
		zap.String("archive", resource.LocalPath),
		zap.String("extract_dir", extractDir),
		zap.Int("extracted_files", len(extractedResources)))

	return extractedResources, nil
}

// extractTarGz 解压 tar.gz 文件
func (rlc *ResourceLocalizationCallback) extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	return rlc.extractTarReader(gzReader, destDir)
}

// extractTar 解压 tar 文件
func (rlc *ResourceLocalizationCallback) extractTar(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return rlc.extractTarReader(file, destDir)
}

// extractTarReader 从 tar reader 解压
func (rlc *ResourceLocalizationCallback) extractTarReader(reader io.Reader, destDir string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)

		// 安全检查：防止路径遍历
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := rlc.extractTarFile(tarReader, target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}

// extractTarFile 解压单个 tar 文件
func (rlc *ResourceLocalizationCallback) extractTarFile(tarReader *tar.Reader, target string, mode os.FileMode) error {
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, tarReader)
	return err
}

// extractZip 解压 zip 文件
func (rlc *ResourceLocalizationCallback) extractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		target := filepath.Join(destDir, file.Name)

		// 安全检查：防止路径遍历
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if file.FileInfo().IsDir() {
			os.MkdirAll(target, file.FileInfo().Mode())
			continue
		}

		if err := rlc.extractZipFile(file, target); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile 解压单个 zip 文件
func (rlc *ResourceLocalizationCallback) extractZipFile(file *zip.File, target string) error {
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, file.FileInfo().Mode())
	if err != nil {
		return err
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, reader)
	return err
}

// processPattern 处理模式匹配
func (rlc *ResourceLocalizationCallback) processPattern(resource *LocalResource) ([]*LocalResource, error) {
	rlc.logger.Debug("Processing pattern", zap.String("pattern", resource.LocalPath))

	// 这里简化处理，实际实现可能需要更复杂的模式匹配逻辑
	// 例如支持通配符、正则表达式等

	var matchedResources []*LocalResource

	// 检查文件是否存在
	if _, err := os.Stat(resource.LocalPath); err == nil {
		// 文件存在，直接返回
		matchedResources = append(matchedResources, resource)
	} else {
		// 尝试目录匹配
		dir := filepath.Dir(resource.LocalPath)
		pattern := filepath.Base(resource.LocalPath)

		if files, err := filepath.Glob(filepath.Join(dir, pattern)); err == nil {
			for _, file := range files {
				if stat, err := os.Stat(file); err == nil && !stat.IsDir() {
					matchedResource := &LocalResource{
						URL:        "pattern://" + file,
						LocalPath:  file,
						Type:       ResourceTypeFile,
						Size:       stat.Size(),
						Visibility: resource.Visibility,
						Downloaded: true,
					}
					matchedResources = append(matchedResources, matchedResource)
				}
			}
		}
	}

	rlc.logger.Debug("Pattern processing completed",
		zap.String("pattern", resource.LocalPath),
		zap.Int("matched_files", len(matchedResources)))

	return matchedResources, nil
}

// setFilePermissions 设置文件权限
func (rlc *ResourceLocalizationCallback) setFilePermissions(resource *LocalResource) error {
	var mode os.FileMode

	switch resource.Visibility {
	case ResourceVisibilityPrivate:
		mode = 0600 // 只有所有者可读写
	case ResourceVisibilityApplication:
		mode = 0640 // 所有者可读写，组可读
	case ResourceVisibilityPublic:
		mode = 0644 // 所有者可读写，其他人可读
	default:
		mode = 0600
	}

	return os.Chmod(resource.LocalPath, mode)
}

// GetLocalizedResourcePath 获取本地化资源路径
func (rl *ResourceLocalizer) GetLocalizedResourcePath(requestID, resourceName string) (string, error) {
	request, err := rl.localizer.GetLocalizationStatus(requestID)
	if err != nil {
		return "", err
	}

	if request.State != LocalizationStateCompleted {
		return "", fmt.Errorf("localization not completed, current state: %s", request.State.String())
	}

	// 查找指定名称的资源
	for _, resource := range request.Resources {
		if filepath.Base(resource.URL) == resourceName || filepath.Base(resource.LocalPath) == resourceName {
			return resource.LocalPath, nil
		}
	}

	return "", fmt.Errorf("resource not found: %s", resourceName)
}

// GetAllLocalizedPaths 获取所有本地化路径
func (rl *ResourceLocalizer) GetAllLocalizedPaths(requestID string) ([]string, error) {
	request, err := rl.localizer.GetLocalizationStatus(requestID)
	if err != nil {
		return nil, err
	}

	if request.State != LocalizationStateCompleted {
		return nil, fmt.Errorf("localization not completed, current state: %s", request.State.String())
	}

	var paths []string
	for _, resource := range request.Resources {
		if resource.Downloaded {
			paths = append(paths, resource.LocalPath)
		}
	}

	return paths, nil
}
