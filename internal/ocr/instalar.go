package ocr

import (
	"os"
	"runtime"
	"strings"
)

// DicaInstalacao devolve o comando para instalar tesseract (com português) e poppler
// no sistema atual.
func DicaInstalacao() string {
	if runtime.GOOS == "darwin" {
		return "brew install tesseract tesseract-lang poppler"
	}
	b, _ := os.ReadFile("/etc/os-release")
	ids := ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "ID=") || strings.HasPrefix(l, "ID_LIKE=") {
			ids += " " + strings.Trim(strings.SplitN(l, "=", 2)[1], `"`)
		}
	}
	switch {
	case strings.Contains(ids, "debian") || strings.Contains(ids, "ubuntu"):
		return "sudo apt install tesseract-ocr tesseract-ocr-por poppler-utils"
	case strings.Contains(ids, "fedora") || strings.Contains(ids, "rhel") || strings.Contains(ids, "centos"):
		return "sudo dnf install tesseract tesseract-langpack-por poppler-utils"
	case strings.Contains(ids, "alpine"):
		return "sudo apk add tesseract-ocr tesseract-ocr-data-por poppler-utils"
	case strings.Contains(ids, "arch"):
		return "sudo pacman -S tesseract tesseract-data-por poppler"
	case strings.Contains(ids, "suse"):
		return "sudo zypper install tesseract-ocr tesseract-ocr-traineddata-portuguese poppler-tools"
	}
	return "instale tesseract (com o idioma português) e poppler pelo gerenciador de pacotes do sistema"
}
