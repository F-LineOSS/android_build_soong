// Copyright 2021 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package java

import (
	"android/soong/android"
	"android/soong/dexpreopt"
	"github.com/google/blueprint"
)

func init() {
	registerPlatformBootclasspathBuildComponents(android.InitRegistrationContext)
}

func registerPlatformBootclasspathBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("platform_bootclasspath", platformBootclasspathFactory)

	ctx.FinalDepsMutators(func(ctx android.RegisterMutatorsContext) {
		ctx.BottomUp("platform_bootclasspath_deps", platformBootclasspathDepsMutator)
	})
}

type platformBootclasspathDependencyTag struct {
	blueprint.BaseDependencyTag

	name string
}

// Avoid having to make platform bootclasspath content visible to the platform bootclasspath.
//
// This is a temporary workaround to make it easier to migrate to platform bootclasspath with proper
// dependencies.
// TODO(b/177892522): Remove this and add needed visibility.
func (t platformBootclasspathDependencyTag) ExcludeFromVisibilityEnforcement() {
}

// The tag used for the dependency between the platform bootclasspath and any configured boot jars.
var platformBootclasspathModuleDepTag = platformBootclasspathDependencyTag{name: "module"}

var _ android.ExcludeFromVisibilityEnforcementTag = platformBootclasspathDependencyTag{}

type platformBootclasspathModule struct {
	android.ModuleBase

	// The apex:module pairs obtained from the configured modules.
	//
	// Currently only for testing.
	configuredModules []android.Module
}

func platformBootclasspathFactory() android.Module {
	m := &platformBootclasspathModule{}
	android.InitAndroidArchModule(m, android.DeviceSupported, android.MultilibCommon)
	return m
}

func (b *platformBootclasspathModule) DepsMutator(ctx android.BottomUpMutatorContext) {
	if SkipDexpreoptBootJars(ctx) {
		return
	}

	// Add a dependency onto the dex2oat tool which is needed for creating the boot image. The
	// path is retrieved from the dependency by GetGlobalSoongConfig(ctx).
	dexpreopt.RegisterToolDeps(ctx)
}

func platformBootclasspathDepsMutator(ctx android.BottomUpMutatorContext) {
	m := ctx.Module()
	if p, ok := m.(*platformBootclasspathModule); ok {
		// Add dependencies on all the modules configured in the "art" boot image.
		artImageConfig := genBootImageConfigs(ctx)[artBootImageName]
		addDependenciesOntoBootImageModules(ctx, artImageConfig.modules)

		// Add dependencies on all the modules configured in the "boot" boot image. That does not
		// include modules configured in the "art" boot image.
		bootImageConfig := p.getImageConfig(ctx)
		addDependenciesOntoBootImageModules(ctx, bootImageConfig.modules)

		// Add dependencies on all the updatable modules.
		updatableModules := dexpreopt.GetGlobalConfig(ctx).UpdatableBootJars
		addDependenciesOntoBootImageModules(ctx, updatableModules)
	}
}

func addDependencyOntoApexModulePair(ctx android.BottomUpMutatorContext, apex string, name string, tag blueprint.DependencyTag) {
	var variations []blueprint.Variation
	if apex != "platform" {
		// Pick the correct apex variant.
		variations = []blueprint.Variation{
			{Mutator: "apex", Variation: apex},
		}
	}

	addedDep := false
	if ctx.OtherModuleDependencyVariantExists(variations, name) {
		ctx.AddFarVariationDependencies(variations, tag, name)
		addedDep = true
	}

	// Add a dependency on the prebuilt module if it exists.
	prebuiltName := android.PrebuiltNameFromSource(name)
	if ctx.OtherModuleDependencyVariantExists(variations, prebuiltName) {
		ctx.AddVariationDependencies(variations, tag, prebuiltName)
		addedDep = true
	}

	// If no appropriate variant existing for this, so no dependency could be added, then it is an
	// error, unless missing dependencies are allowed. The simplest way to handle that is to add a
	// dependency that will not be satisfied and the default behavior will handle it.
	if !addedDep {
		ctx.AddFarVariationDependencies(variations, tag, name)
	}
}

func addDependenciesOntoBootImageModules(ctx android.BottomUpMutatorContext, modules android.ConfiguredJarList) {
	for i := 0; i < modules.Len(); i++ {
		apex := modules.Apex(i)
		name := modules.Jar(i)

		addDependencyOntoApexModulePair(ctx, apex, name, platformBootclasspathModuleDepTag)
	}
}

func (b *platformBootclasspathModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	ctx.VisitDirectDepsIf(isActiveModule, func(module android.Module) {
		tag := ctx.OtherModuleDependencyTag(module)
		if tag == platformBootclasspathModuleDepTag {
			b.configuredModules = append(b.configuredModules, module)
		}
	})

	// Nothing to do if skipping the dexpreopt of boot image jars.
	if SkipDexpreoptBootJars(ctx) {
		return
	}

	// Force the GlobalSoongConfig to be created and cached for use by the dex_bootjars
	// GenerateSingletonBuildActions method as it cannot create it for itself.
	dexpreopt.GetGlobalSoongConfig(ctx)

	imageConfig := b.getImageConfig(ctx)
	if imageConfig == nil {
		return
	}

	// Construct the boot image info from the config.
	info := BootImageInfo{imageConfig: imageConfig}

	// Make it available for other modules.
	ctx.SetProvider(BootImageInfoProvider, info)
}

func (b *platformBootclasspathModule) getImageConfig(ctx android.EarlyModuleContext) *bootImageConfig {
	return defaultBootImageConfig(ctx)
}