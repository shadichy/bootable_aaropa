package aaropa

import (
	"android/soong/android"
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("aaropa_newinstaller", NewInstallerFactory)
}

var (
	pctx = android.NewPackageContext("android/soong/aaropa/newinstaller")
)

type hostToolDependencyTag struct {
	blueprint.BaseDependencyTag
}

var hostToolDepTag hostToolDependencyTag

type newInstallerProperties struct {
	Inline *bool
}

type newInstallerModule struct {
	android.ModuleBase
	properties newInstallerProperties

	outputFile android.Path
}

var _ android.Module = (*newInstallerModule)(nil)

func NewInstallerFactory() android.Module {
	module := &newInstallerModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidModule(module)
	return module
}

func (m *newInstallerModule) DepsMutator(ctx android.BottomUpMutatorContext) {
	// Add far dependency on toybox and acp host tools
	ctx.AddFarVariationDependencies(ctx.Config().BuildOSTarget.Variations(), hostToolDepTag, "toybox", "acp")
}

func (m *newInstallerModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	m.outputFile = android.PathForModuleOut(ctx, "install.img")

	// 1. Resolve host tools
	var toyboxPath android.Path
	var acpPath android.Path
	ctx.VisitDirectDepsWithTag(hostToolDepTag, func(dep android.Module) {
		if ctx.OtherModuleName(dep) == "toybox" {
			if hostTool, ok := dep.(android.HostToolProvider); ok {
				toyboxPath = hostTool.HostToolPath().Path()
			}
		}
		if ctx.OtherModuleName(dep) == "acp" {
			if hostTool, ok := dep.(android.HostToolProvider); ok {
				acpPath = hostTool.HostToolPath().Path()
			}
		}
	})

	if toyboxPath == nil {
		ctx.PropertyErrorf("toybox", "failed to find toybox host tool")
		return
	}
	if acpPath == nil {
		ctx.PropertyErrorf("acp", "failed to find acp host tool")
		return
	}

	// 2. Establish rule builder
	rule := android.NewRuleBuilder(pctx, ctx)

	stagingDir := android.PathForModuleOut(ctx, "staging_newinstaller")

	// Prepare directories and copy inputs
	rule.Command().Text("rm -rf").Text(stagingDir.String())
	rule.Command().Text("mkdir -p").
		Text(stagingDir.String() + "/android").
		Text(stagingDir.String() + "/apex").
		Text(stagingDir.String() + "/dev").
		Text(stagingDir.String() + "/proc").
		Text(stagingDir.String() + "/sys").
		Text(stagingDir.String() + "/tmp").
		Text(stagingDir.String() + "/etc").
		Text(stagingDir.String() + "/data").
		Text(stagingDir.String() + "/cdrom").
		Text(stagingDir.String() + "/boot").
		Text(stagingDir.String() + "/source").
		Text(stagingDir.String() + "/hd").
		Text(stagingDir.String() + "/var/lib/os-prober/mount")

	rule.Command().Text("touch").Text(stagingDir.String() + "/etc/fstab")

	if proptools.Bool(m.properties.Inline) {
		inlineDir := android.PathForSource(ctx, "bootable/newinstaller/install")
		rule.Command().Tool(acpPath).Text("-dpr").Text(inlineDir.String() + "/.").Text(stagingDir.String() + "/")
	}

	// Run find | cpio | gzip via bash pipeline calling the required toybox host tools
	rule.Command().
		Text("cd").Text(stagingDir.String()).
		Text("&&").
		Text("find . |").
		Tool(toyboxPath).Text("cpio -o |").
		Tool(toyboxPath).Text("gzip -9 >").
		Output(m.outputFile.(android.WritablePath))

	rule.Build("build_aaropa_newinstaller", "Building Aaropa install.img (newinstaller) via Toybox")

	ctx.SetOutputFiles(android.Paths{m.outputFile}, "")
}
