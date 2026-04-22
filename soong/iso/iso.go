package aaropa

import (
	"strings"
	"time"

	"android/soong/android"
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

var pctx = android.NewPackageContext("android/soong/aaropa/iso")

func init() {
	android.RegisterModuleType("aaropa_iso", IsoFactory)
}

type isoProperties struct {
	Boot_hybrid      string
	Os_title         string
	Disklabel        string
	Use_newinstaller *bool
}

type isoModule struct {
	android.ModuleBase
	properties isoProperties

	outputFile android.Path
}

var _ android.Module = (*isoModule)(nil)

func IsoFactory() android.Module {
	module := &isoModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidModule(module)
	return module
}

type isoHostToolDepTag struct {
	blueprint.BaseDependencyTag
}

var isoHostToolTag isoHostToolDepTag

type initrdDepTag struct {
	blueprint.BaseDependencyTag
}

type newInstallerDepTag struct {
	blueprint.BaseDependencyTag
}

func (m *isoModule) DepsMutator(ctx android.BottomUpMutatorContext) {
	// Add far dependency on xorriso and acp
	ctx.AddFarVariationDependencies(ctx.Config().BuildOSTarget.Variations(), isoHostToolTag, "xorriso", "acp")
	// Depend on the initrd.img intermediate artifact explicitly
	ctx.AddDependency(ctx.Module(), initrdDepTag{}, "initrd.img")

	useNewInstaller := proptools.Bool(m.properties.Use_newinstaller)
	if !useNewInstaller {
		envVal := ctx.Config().Getenv("USE_NEWINSTALLER")
		useNewInstaller = envVal == "true" || (envVal != "" && envVal != "0")
	}

	if useNewInstaller {
		ctx.AddDependency(ctx.Module(), newInstallerDepTag{}, "newinstaller.img")
	}
}

func (m *isoModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	isoName := ctx.Config().Getenv("BLISS_BUILD_ZIP")
	if isoName == "" {
		isoName = "aaropa_generic"
	}

	m.outputFile = android.PathForModuleOut(ctx, isoName+".iso")

	var xorrisoPath android.Path
	var acpPath android.Path
	ctx.VisitDirectDepsWithTag(isoHostToolTag, func(dep android.Module) {
		if ctx.OtherModuleName(dep) == "xorriso" {
			if hostTool, ok := dep.(android.HostToolProvider); ok {
				xorrisoPath = hostTool.HostToolPath().Path()
			}
		}
		if ctx.OtherModuleName(dep) == "acp" {
			if hostTool, ok := dep.(android.HostToolProvider); ok {
				acpPath = hostTool.HostToolPath().Path()
			}
		}
	})

	var initrdImg android.Path
	var newinstallerImg android.Path
	ctx.VisitDirectDepsWithTag(initrdDepTag{}, func(dep android.Module) {
		if ctx.OtherModuleName(dep) == "initrd.img" {
			out := android.OutputFileForModule(ctx, dep, "")
			if out != nil {
				initrdImg = out
			}
		}
	})
	ctx.VisitDirectDepsWithTag(newInstallerDepTag{}, func(dep android.Module) {
		if ctx.OtherModuleName(dep) == "newinstaller.img" {
			out := android.OutputFileForModule(ctx, dep, "")
			if out != nil {
				newinstallerImg = out
			}
		}
	})

	if xorrisoPath == nil {
		ctx.PropertyErrorf("xorriso", "failed to resolve xorriso host tool")
		return
	}
	if acpPath == nil {
		ctx.PropertyErrorf("acp", "failed to resolve acp host tool")
		return
	}
	if initrdImg == nil {
		ctx.PropertyErrorf("initrd", "failed to resolve initrd.img module dependency")
		return
	}

	rule := android.NewRuleBuilder(pctx, ctx)

	stagingDir := android.PathForModuleOut(ctx, "iso_staging")
	srcDir := android.PathForSource(ctx, "prebuilts/aaropa/iso")
	isoDir := srcDir.Join(ctx, "iso")

	// Resolving generic fallback mappings natively internally
	osTitle := m.properties.Os_title
	if osTitle == "" {
		osTitle = "Android-x86"
	}

	boardCmdline := ctx.Config().Getenv("BOARD_KERNEL_CMDLINE")
	if boardCmdline == "" {
		boardCmdline = "quiet"
	}

	diskLabel := m.properties.Disklabel
	if len(diskLabel) > 5 {
		diskLabel = diskLabel[:5]
	} else if len(diskLabel) == 0 {
		diskLabel = "A86xx"
	}

	// Append Date: YYjjj natively in Go
	diskLabel = strings.ToUpper(diskLabel) + "_" + time.Now().Format("06002")
	modDate := time.Now().Format("2006010215040500")

	useNewInstaller := proptools.Bool(m.properties.Use_newinstaller)
	if !useNewInstaller {
		envVal := ctx.Config().Getenv("USE_NEWINSTALLER")
		useNewInstaller = envVal == "true" || (envVal != "" && envVal != "0")
	}

	// Just for testing, let's log the value or reflect it in a command if needed
	// For now we just implement the toggle logic as requested.

	rule.Command().Text("rm -rf").Text(stagingDir.String())
	// Copy the ISO staging payload statically to outputs
	rule.Command().Tool(acpPath).Text("-pr").Text(isoDir.String() + "/.").Text(stagingDir.String() + "/")

	// Copy the extracted initrd.img artifact into the staging payload
	rule.Command().Tool(acpPath).Input(initrdImg).Text(stagingDir.String() + "/initrd.img")

	if newinstallerImg != nil {
		rule.Command().Tool(acpPath).Input(newinstallerImg).Text(stagingDir.String() + "/newinstaller.img")
	}

	// Mutate grub.cfg via internal scripts
	rule.Command().Text("sed -i").
		Text("-e \"s|OS_TITLE|" + osTitle + "|\"").
		Text("-e \"s|BlissOSLive|" + diskLabel + "|\"").
		Text("-e \"s|CMDLINE|" + boardCmdline + "|\"").
		Text(stagingDir.String() + "/boot/grub/grub.cfg")

	bootHybrid := isoDir.Join(ctx, "boot_hybrid.img")

	rule.Command().
		Tool(xorrisoPath).
		Text("-as mkisofs -graft-points").
		Text("--modification-date=" + modDate).
		Text("-b /boot/grub/i386-pc/eltorito.img -no-emul-boot -boot-load-size 4 -boot-info-table").
		Text("--grub2-boot-info --grub2-mbr").Input(bootHybrid).
		Text("-hfsplus -apm-block-size 2048 -hfsplus-file-creator-type chrp tbxj /System/Library/CoreServices/.disk_label").
		Text("-hfs-bless-by i /System/Library/CoreServices/boot.efi --efi-boot efi.img -efi-boot-part --efi-boot-image").
		Text("--protective-msdos-label -o").Output(m.outputFile.(android.WritablePath)).
		Text(stagingDir.String()).
		Text("--sort-weight 0 / --sort-weight 1 /boot").
		Text("-V '" + diskLabel + "' -- -volid_for hfsplus '" + diskLabel + "_HFS'")

	rule.Build("build_aaropa_iso", "Building Aaropa ISO via Xorriso")
}
