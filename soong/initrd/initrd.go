package aaropa

import (
	"android/soong/android"
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterModuleType("aaropa_initrd", InitrdFactory)
}

type initrdProperties struct {
	Os_title string
	Ver      string
	Inline   *bool
}

type initrdModule struct {
	android.ModuleBase
	properties initrdProperties

	outputFile android.Path
}

var _ android.Module = (*initrdModule)(nil)

func InitrdFactory() android.Module {
	module := &initrdModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidModule(module)
	return module
}

var (
	pctx = android.NewPackageContext("android/soong/aaropa")
)

type hostToolDependencyTag struct {
	blueprint.BaseDependencyTag
}

var hostToolDepTag hostToolDependencyTag

func (m *initrdModule) DepsMutator(ctx android.BottomUpMutatorContext) {
	// Add far dependency on toybox and acp host tools
	ctx.AddFarVariationDependencies(ctx.Config().BuildOSTarget.Variations(), hostToolDepTag, "toybox", "acp")
}

func (m *initrdModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	m.outputFile = android.PathForModuleOut(ctx, "initrd.img")

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

	// Since bootable/aaropa manually merges initrd_lib into initrd before generating, we use that folder.
	srcDir := android.PathForSource(ctx, "prebuilts/aaropa/initrd")
	initrdDir := srcDir.Join(ctx, "initrd")

	// Create the 00-ver script natively in intermediates using Go
	verScriptOut := android.PathForModuleOut(ctx, "00-ver")

	verStr := m.properties.Ver
	if verStr == "" {
		verStr = "15" // Fallback default
	}

	content := "VER=" + verStr + "\n"
	if m.properties.Os_title != "" {
		content += "OS_TITLE=" + m.properties.Os_title + "\n"
	}

	android.WriteFileRule(ctx, verScriptOut, content)

	stagingDir := android.PathForModuleOut(ctx, "staging_initrd")

	// Prepare directories and copy inputs
	rule.Command().Text("rm -rf").Text(stagingDir.String())
	rule.Command().Text("mkdir -p").
		Text(stagingDir.String() + "/android").
		Text(stagingDir.String() + "/apex").
		Text(stagingDir.String() + "/mnt").
		Text(stagingDir.String() + "/proc").
		Text(stagingDir.String() + "/sys").
		Text(stagingDir.String() + "/tmp")

	rule.Command().Tool(acpPath).Text("-dpr").Text(initrdDir.String() + "/.").Text(stagingDir.String() + "/")

	if proptools.Bool(m.properties.Inline) {
		inlineDir := android.PathForSource(ctx, "bootable/aaropa/initrd")
		rule.Command().Tool(acpPath).Text("-dpr").Text(inlineDir.String() + "/.").Text(stagingDir.String() + "/")
	}

	rule.Command().Tool(acpPath).Text("-p").Input(verScriptOut).Text(stagingDir.String() + "/scripts/00-ver")

	// Run find | cpio | gzip via bash pipeline calling the required toybox host tools
	rule.Command().
		Text("cd").Text(stagingDir.String()).
		Text("&&").
		Text("find . |").
		Tool(toyboxPath).Text("cpio -o |").
		Tool(toyboxPath).Text("gzip -9 >").
		Output(m.outputFile.(android.WritablePath))

	rule.Build("build_aaropa_initrd", "Building Aaropa initrd.img via Toybox")

	ctx.SetOutputFiles(android.Paths{m.outputFile}, "")
}
