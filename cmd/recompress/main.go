// Recompresses all WebP files in the uploads directory using cwebp.
// Usage: go run ./cmd/recompress [--dir ./uploads] [--quality 75] [--dry-run]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	dir := flag.String("dir", "./uploads", "directory with uploaded files")
	quality := flag.Int("quality", 75, "WebP quality (0-100)")
	dryRun := flag.Bool("dry-run", false, "print what would be done without changing files")
	flag.Parse()

	// Verify cwebp is available
	if _, err := exec.LookPath("cwebp"); err != nil {
		log.Fatal("cwebp not found — install with: brew install webp")
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir %s: %v", *dir, err)
	}

	var totalBefore, totalAfter int64
	var processed, skipped int

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".webp" {
			continue
		}

		path := filepath.Join(*dir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			log.Printf("stat %s: %v", path, err)
			continue
		}
		before := info.Size()

		if *dryRun {
			fmt.Printf("[dry-run] would recompress %s (%d KB)\n", e.Name(), before/1024)
			continue
		}

		tmp := path + ".tmp.webp"
		cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", *quality), "-quiet", path, "-o", tmp)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("cwebp %s: %v — %s", e.Name(), err, out)
			os.Remove(tmp)
			skipped++
			continue
		}

		afterInfo, err := os.Stat(tmp)
		if err != nil {
			log.Printf("stat tmp %s: %v", tmp, err)
			os.Remove(tmp)
			skipped++
			continue
		}
		after := afterInfo.Size()

		// Don't replace if recompressed file is larger
		if after >= before {
			os.Remove(tmp)
			fmt.Printf("skip  %s — already optimal (%d KB)\n", e.Name(), before/1024)
			skipped++
			continue
		}

		if err := os.Rename(tmp, path); err != nil {
			log.Printf("rename %s: %v", e.Name(), err)
			os.Remove(tmp)
			skipped++
			continue
		}

		saving := (before - after) * 100 / before
		fmt.Printf("ok    %s  %d KB → %d KB  (-%d%%)\n", e.Name(), before/1024, after/1024, saving)
		totalBefore += before
		totalAfter += after
		processed++
	}

	fmt.Printf("\nОбработано: %d, пропущено: %d\n", processed, skipped)
	if processed > 0 {
		totalSaving := (totalBefore - totalAfter) * 100 / totalBefore
		fmt.Printf("Итого: %d MB → %d MB (-%d%%)\n",
			totalBefore/1024/1024, totalAfter/1024/1024, totalSaving)
	}
}
