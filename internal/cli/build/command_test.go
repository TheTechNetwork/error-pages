package build_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gh.tarampamp.am/error-pages/internal/cli/build"
	"gh.tarampamp.am/error-pages/internal/config"
	"gh.tarampamp.am/error-pages/internal/logger"
)

func TestNewCommand(t *testing.T) {
	t.Parallel()

	var cmd = build.NewCommand(logger.NewNop())

	assert.NotNil(t, cmd)
	assert.Equal(t, "build", cmd.Name)
	assert.Contains(t, cmd.Aliases, "b")
	assert.NotEmpty(t, cmd.Usage)
	assert.NotEmpty(t, cmd.Flags)
}

func TestCommand_Run_Basic(t *testing.T) {
	t.Parallel()

	var (
		ctx     = context.Background()
		log     = logger.NewNop()
		tempDir = t.TempDir()
	)

	tests := []struct {
		name           string
		args           []string
		modifyConfig   func(*config.Config)
		validateResult func(t *testing.T, dir string)
		wantErr        bool
		wantErrMsg     string
	}{
		{
			name: "basic build with default templates",
			args: []string{"build", "--target-dir", tempDir},
			validateResult: func(t *testing.T, dir string) {
				// Check that at least one template directory was created
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.NotEmpty(t, entries, "expected at least one template directory")

				// Check that HTML files were created
				var foundHtmlFiles bool
				for _, entry := range entries {
					if entry.IsDir() {
						htmlFiles, _ := filepath.Glob(filepath.Join(dir, entry.Name(), "*.html"))
						if len(htmlFiles) > 0 {
							foundHtmlFiles = true
							break
						}
					}
				}
				assert.True(t, foundHtmlFiles, "expected HTML files to be generated")
			},
		},
		{
			name: "build with index file",
			args: []string{"build", "--target-dir", tempDir, "--index"},
			validateResult: func(t *testing.T, dir string) {
				indexPath := filepath.Join(dir, "index.html")
				assert.FileExists(t, indexPath, "index.html should exist")

				content, err := os.ReadFile(indexPath)
				require.NoError(t, err)

				contentStr := string(content)
				assert.Contains(t, contentStr, "Error pages index")
				assert.Contains(t, contentStr, "Template name:")
			},
		},
		{
			name: "build with disabled minification",
			args: []string{"build", "--target-dir", tempDir, "--disable-minification"},
			validateResult: func(t *testing.T, dir string) {
				// Find any generated HTML file
				var htmlFile string
				err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
					if err == nil && !info.IsDir() && strings.HasSuffix(path, ".html") && !strings.Contains(path, "index.html") {
						htmlFile = path
						return filepath.SkipAll
					}
					return nil
				})
				require.NoError(t, err)
				require.NotEmpty(t, htmlFile, "expected to find an HTML file")

				content, err := os.ReadFile(htmlFile)
				require.NoError(t, err)

				// Non-minified HTML should contain newlines
				assert.Contains(t, string(content), "\n")
			},
		},
		{
			name: "build with disabled l10n",
			args: []string{"build", "--target-dir", tempDir, "--disable-l10n"},
			validateResult: func(t *testing.T, dir string) {
				// Verify files are created (l10n flag affects content but shouldn't break build)
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.NotEmpty(t, entries)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				testDir = t.TempDir()
				cmd     = build.NewCommand(log)
			)

			// Replace the tempDir placeholder with the actual test directory
			args := make([]string, len(tt.args))
			for i, arg := range tt.args {
				if arg == tempDir {
					args[i] = testDir
				} else {
					args[i] = arg
				}
			}

			err := cmd.Run(ctx, args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				require.NoError(t, err)
				if tt.validateResult != nil {
					tt.validateResult(t, testDir)
				}
			}
		})
	}
}

func TestCommand_Run_CustomTemplates(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("add custom template from file", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// Create a custom template file
		customTplPath := filepath.Join(testDir, "custom.html")
		customContent := `<!DOCTYPE html><html><body><h1>Error {{.Code}}</h1></body></html>`
		require.NoError(t, os.WriteFile(customTplPath, []byte(customContent), 0644))

		var outDir = filepath.Join(testDir, "out")
		require.NoError(t, os.Mkdir(outDir, 0755))

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", outDir,
			"--add-template", customTplPath,
		})
		require.NoError(t, err)

		// Verify custom template directory was created
		customDir := filepath.Join(outDir, "custom")
		assert.DirExists(t, customDir)

		// Verify HTML files were generated in the custom template directory
		htmlFiles, err := filepath.Glob(filepath.Join(customDir, "*.html"))
		require.NoError(t, err)
		assert.NotEmpty(t, htmlFiles, "expected HTML files in custom template directory")
	})

	t.Run("add custom template with invalid path", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-template", "/nonexistent/template.html",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot add template")
	})

	t.Run("disable built-in template", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--disable-template", "ghost",
		})
		require.NoError(t, err)

		// Verify ghost template directory was NOT created
		ghostDir := filepath.Join(testDir, "ghost")
		_, err = os.Stat(ghostDir)
		assert.True(t, os.IsNotExist(err), "ghost directory should not exist")
	})

	t.Run("disable all templates should fail", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// Get all template names and disable them all
		cfg := config.New()
		var disableArgs []string
		for _, name := range cfg.Templates.Names() {
			disableArgs = append(disableArgs, "--disable-template", name)
		}

		args := append([]string{"build", "--target-dir", testDir}, disableArgs...)

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no templates specified")
	})
}

func TestCommand_Run_CustomCodes(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("add custom HTTP code", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-code", "599=Custom Error/Custom error description",
		})
		require.NoError(t, err)

		// Verify 599.html was created in at least one template directory
		var found599 bool
		err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "599.html") {
				found599 = true
				return filepath.SkipAll
			}
			return nil
		})
		require.NoError(t, err)
		assert.True(t, found599, "expected to find 599.html")
	})

	t.Run("add multiple custom codes", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-code", "597=Custom 1/Description 1",
			"--add-code", "598=Custom 2/Description 2",
		})
		require.NoError(t, err)

		// Verify both custom codes were created
		var found597, found598 bool
		err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				if strings.HasSuffix(path, "597.html") {
					found597 = true
				}
				if strings.HasSuffix(path, "598.html") {
					found598 = true
				}
			}
			return nil
		})
		require.NoError(t, err)
		assert.True(t, found597, "expected to find 597.html")
		assert.True(t, found598, "expected to find 598.html")
	})
}

func TestCommand_Run_IndexGeneration(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("index file contains all templates", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--index",
		})
		require.NoError(t, err)

		indexPath := filepath.Join(testDir, "index.html")
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)

		contentStr := string(content)

		// Get default config to check template names
		cfg := config.New()
		templateNames := cfg.Templates.Names()

		// Verify index contains references to all templates
		for _, name := range templateNames {
			assert.Contains(t, contentStr, name, "index should contain template name: "+name)
		}
	})

	t.Run("index file contains sorted codes", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--index",
			"--add-code", "599=Last/Description",
			"--add-code", "400=First/Description",
		})
		require.NoError(t, err)

		indexPath := filepath.Join(testDir, "index.html")
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)

		contentStr := string(content)

		// Find positions of codes in the HTML
		pos400 := strings.Index(contentStr, ">400<")
		pos599 := strings.Index(contentStr, ">599<")

		// 400 should appear before 599 (sorted order)
		if pos400 >= 0 && pos599 >= 0 {
			assert.Less(t, pos400, pos599, "codes should be sorted in index")
		}
	})

	t.Run("index file contains relative paths", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--index",
		})
		require.NoError(t, err)

		indexPath := filepath.Join(testDir, "index.html")
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)

		contentStr := string(content)

		// Verify paths are relative (start with ./)
		assert.Contains(t, contentStr, "href=\"./")
		// Should not contain absolute paths
		assert.NotContains(t, contentStr, "href=\""+testDir)
	})
}

func TestCommand_Run_ErrorHandling(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("invalid target directory", func(t *testing.T) {
		t.Parallel()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", "/nonexistent/directory/path",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot access the target directory")
	})

	t.Run("target is file not directory", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()
		filePath := filepath.Join(testDir, "not-a-dir.txt")
		require.NoError(t, os.WriteFile(filePath, []byte("test"), 0644))

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", filePath,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("empty target directory flag", func(t *testing.T) {
		t.Parallel()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing target directory")
	})
}

func TestCommand_Run_FilePermissions(t *testing.T) {
	t.Parallel()

	// Skip this test on Windows as it has different permission handling
	if os.Getenv("GOOS") == "windows" {
		t.Skip("skipping file permission test on Windows")
	}

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("verify output file permissions", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
		})
		require.NoError(t, err)

		// Find a generated HTML file
		var htmlFile string
		err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".html") {
				htmlFile = path
				return filepath.SkipAll
			}
			return nil
		})
		require.NoError(t, err)
		require.NotEmpty(t, htmlFile, "expected to find an HTML file")

		// Check file permissions
		info, err := os.Stat(htmlFile)
		require.NoError(t, err)

		// Should be readable by owner and group (0664)
		mode := info.Mode()
		assert.True(t, mode&0400 != 0, "file should be readable by owner")
		assert.True(t, mode&0200 != 0, "file should be writable by owner")
		assert.True(t, mode&0040 != 0, "file should be readable by group")
	})
}

func TestCommand_Run_ContentValidation(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("generated HTML contains error code", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
		})
		require.NoError(t, err)

		// Find 404.html in any template directory
		var htmlFile string
		err = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "404.html") {
				htmlFile = path
				return filepath.SkipAll
			}
			return nil
		})
		require.NoError(t, err)
		require.NotEmpty(t, htmlFile, "expected to find 404.html")

		content, err := os.ReadFile(htmlFile)
		require.NoError(t, err)

		contentStr := string(content)
		// Should contain 404 somewhere in the HTML
		assert.Contains(t, contentStr, "404")
	})

	t.Run("minified HTML is smaller", func(t *testing.T) {
		t.Parallel()

		var testDir1 = t.TempDir()
		var testDir2 = t.TempDir()

		// Build with minification (default)
		cmd1 := build.NewCommand(log)
		err := cmd1.Run(ctx, []string{
			"build",
			"--target-dir", testDir1,
		})
		require.NoError(t, err)

		// Build without minification
		cmd2 := build.NewCommand(log)
		err = cmd2.Run(ctx, []string{
			"build",
			"--target-dir", testDir2,
			"--disable-minification",
		})
		require.NoError(t, err)

		// Compare file sizes - minified should generally be smaller
		var minifiedSize, unminifiedSize int64

		// Get first HTML file from minified build
		err = filepath.Walk(testDir1, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".html") && !strings.Contains(path, "index.html") {
				minifiedSize = info.Size()
				return filepath.SkipAll
			}
			return nil
		})
		require.NoError(t, err)

		// Get first HTML file from unminified build
		err = filepath.Walk(testDir2, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".html") && !strings.Contains(path, "index.html") {
				unminifiedSize = info.Size()
				return filepath.SkipAll
			}
			return nil
		})
		require.NoError(t, err)

		assert.Greater(t, minifiedSize, int64(0), "minified file should have content")
		assert.Greater(t, unminifiedSize, int64(0), "unminified file should have content")

		// Minified should typically be smaller or equal (equal if already minimal)
		assert.LessOrEqual(t, minifiedSize, unminifiedSize, "minified file should not be larger than unminified")
	})
}

func TestCommand_Run_RelativePaths(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("relative target directory path", func(t *testing.T) {
		t.Parallel()

		// Create a relative path directory
		var testDir = t.TempDir()
		var relDir = filepath.Join(testDir, "output")
		require.NoError(t, os.Mkdir(relDir, 0755))

		// Change to test directory
		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()

		require.NoError(t, os.Chdir(testDir))

		cmd := build.NewCommand(log)
		err = cmd.Run(ctx, []string{
			"build",
			"--target-dir", "./output",
		})
		require.NoError(t, err)

		// Verify files were created
		entries, err := os.ReadDir(relDir)
		require.NoError(t, err)
		assert.NotEmpty(t, entries, "expected files in output directory")
	})
}

func TestCommand_Run_DirectoryCreation(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("creates template directories", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
		})
		require.NoError(t, err)

		// Verify template directories were created
		cfg := config.New()
		for _, templateName := range cfg.Templates.Names() {
			templateDir := filepath.Join(testDir, templateName)
			info, err := os.Stat(templateDir)
			require.NoError(t, err, "template directory should exist: "+templateName)
			assert.True(t, info.IsDir(), "should be a directory: "+templateName)
		}
	})

	t.Run("idempotent - can run twice in same directory", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd1 := build.NewCommand(log)
		err := cmd1.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
		})
		require.NoError(t, err)

		// Run again - should succeed (idempotent)
		cmd2 := build.NewCommand(log)
		err = cmd2.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
		})
		require.NoError(t, err, "second build should succeed (idempotent)")
	})

	t.Run("fails when template name conflicts with existing file", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// Create a custom template file
		customTplPath := filepath.Join(testDir, "mytemplate.html")
		require.NoError(t, os.WriteFile(customTplPath, []byte(`<html><body>Test</body></html>`), 0644))

		// Create a file with the same name as the template (without extension)
		conflictFile := filepath.Join(testDir, "mytemplate")
		require.NoError(t, os.WriteFile(conflictFile, []byte("conflict"), 0644))

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-template", customTplPath,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("creates nested directory structure", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// Create output directory
		outputDir := filepath.Join(testDir, "nested", "output", "dir")
		require.NoError(t, os.MkdirAll(outputDir, 0755))

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", outputDir,
		})
		require.NoError(t, err)

		// Verify files were created in nested structure
		entries, err := os.ReadDir(outputDir)
		require.NoError(t, err)
		assert.NotEmpty(t, entries)
	})
}

func TestCommand_Run_CombinedFlags(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("all flags combined", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// Create a custom template
		customTplPath := filepath.Join(testDir, "custom.html")
		require.NoError(t, os.WriteFile(customTplPath, []byte(`<!DOCTYPE html><html><body>{{.Code}}</body></html>`), 0644))

		var outDir = filepath.Join(testDir, "out")
		require.NoError(t, os.Mkdir(outDir, 0755))

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", outDir,
			"--add-template", customTplPath,
			"--disable-template", "ghost",
			"--add-code", "599=Custom/Description",
			"--disable-l10n",
			"--disable-minification",
			"--index",
		})
		require.NoError(t, err)

		// Verify custom template directory exists
		assert.DirExists(t, filepath.Join(outDir, "custom"))

		// Verify ghost template does NOT exist
		_, err = os.Stat(filepath.Join(outDir, "ghost"))
		assert.True(t, os.IsNotExist(err))

		// Verify 599.html exists
		var found599 bool
		_ = filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(path, "599.html") {
				found599 = true
				return filepath.SkipAll
			}
			return nil
		})
		assert.True(t, found599)

		// Verify index.html exists
		assert.FileExists(t, filepath.Join(outDir, "index.html"))
	})
}

func TestCommand_Run_EdgeCases(t *testing.T) {
	t.Parallel()

	var (
		ctx = context.Background()
		log = logger.NewNop()
	)

	t.Run("handles unparseable HTTP codes gracefully", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		// This test verifies that if someone adds a wildcard code pattern,
		// it's skipped during build (since strconv.ParseUint will fail)
		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-code", "5**=Server Error/Wildcard pattern",
		})

		// Should not error - just skip unparseable codes
		require.NoError(t, err)

		// Verify 5**.html was NOT created (unparseable)
		var found5Star bool
		_ = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.Contains(path, "5**") {
				found5Star = true
			}
			return nil
		})
		assert.False(t, found5Star, "wildcard code file should not be created")
	})

	t.Run("empty custom code description", func(t *testing.T) {
		t.Parallel()

		var testDir = t.TempDir()

		cmd := build.NewCommand(log)
		err := cmd.Run(ctx, []string{
			"build",
			"--target-dir", testDir,
			"--add-code", "599=/",
		})
		require.NoError(t, err)

		// File should still be created even with empty message/description
		var found599 bool
		_ = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(path, "599.html") {
				found599 = true
				return filepath.SkipAll
			}
			return nil
		})
		assert.True(t, found599)
	})
}
