export namespace config {
	
	export class ResourceItem {
	    path: string;
	    enabled: boolean;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResourceItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.enabled = source["enabled"];
	        this.isDir = source["isDir"];
	    }
	}
	export class BuildConfig {
	    name: string;
	    version: string;
	    author: string;
	    iconPath: string;
	    entryScript: string;
	    oneFile: boolean;
	    noConsole: boolean;
	    liteMode: boolean;
	    resources: ResourceItem[];
	    outputDir: string;
	    contentsDir: string;
	    hiddenImports: string[];
	    useUPX: boolean;
	    excludeModules: string[];
	    encryptMode: string;
	    pythonPath: string;
	    pipMirror: string;
	    extraArgs: string[];
	    cacheAccel: boolean;
	    uacAdmin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BuildConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.iconPath = source["iconPath"];
	        this.entryScript = source["entryScript"];
	        this.oneFile = source["oneFile"];
	        this.noConsole = source["noConsole"];
	        this.liteMode = source["liteMode"];
	        this.resources = this.convertValues(source["resources"], ResourceItem);
	        this.outputDir = source["outputDir"];
	        this.contentsDir = source["contentsDir"];
	        this.hiddenImports = source["hiddenImports"];
	        this.useUPX = source["useUPX"];
	        this.excludeModules = source["excludeModules"];
	        this.encryptMode = source["encryptMode"];
	        this.pythonPath = source["pythonPath"];
	        this.pipMirror = source["pipMirror"];
	        this.extraArgs = source["extraArgs"];
	        this.cacheAccel = source["cacheAccel"];
	        this.uacAdmin = source["uacAdmin"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DepInfo {
	    importName: string;
	    pkgName: string;
	    version: string;
	    installedVersion: string;
	    installed: boolean;
	    required: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new DepInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.importName = source["importName"];
	        this.pkgName = source["pkgName"];
	        this.version = source["version"];
	        this.installedVersion = source["installedVersion"];
	        this.installed = source["installed"];
	        this.required = source["required"];
	        this.source = source["source"];
	    }
	}
	export class DepsResult {
	    missing: DepInfo[];
	    resolved: DepInfo[];
	    hookExtra: DepInfo[];
	    analysisModules: string[];
	    analysisSeconds: number;
	    isCached: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DepsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.missing = this.convertValues(source["missing"], DepInfo);
	        this.resolved = this.convertValues(source["resolved"], DepInfo);
	        this.hookExtra = this.convertValues(source["hookExtra"], DepInfo);
	        this.analysisModules = source["analysisModules"];
	        this.analysisSeconds = source["analysisSeconds"];
	        this.isCached = source["isCached"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PythonEnv {
	    name: string;
	    path: string;
	    version: string;
	    isVirtual: boolean;
	    hasPip: boolean;
	    hasPyInstaller: boolean;
	    hasPyarmor: boolean;
	    isPortable: boolean;
	    root: string;
	
	    static createFrom(source: any = {}) {
	        return new PythonEnv(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.isVirtual = source["isVirtual"];
	        this.hasPip = source["hasPip"];
	        this.hasPyInstaller = source["hasPyInstaller"];
	        this.hasPyarmor = source["hasPyarmor"];
	        this.isPortable = source["isPortable"];
	        this.root = source["root"];
	    }
	}
	export class ResourceDep {
	    ref: string;
	    diskPath: string;
	    exists: boolean;
	    included: boolean;
	    kind: string;
	    sourceFile: string;
	    line: number;
	    code: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceDep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = source["ref"];
	        this.diskPath = source["diskPath"];
	        this.exists = source["exists"];
	        this.included = source["included"];
	        this.kind = source["kind"];
	        this.sourceFile = source["sourceFile"];
	        this.line = source["line"];
	        this.code = source["code"];
	    }
	}

}

export namespace portable {
	
	export class PortablePython {
	    version: string;
	    path: string;
	    dir: string;
	    sizeMB: number;
	
	    static createFrom(source: any = {}) {
	        return new PortablePython(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	        this.sizeMB = source["sizeMB"];
	    }
	}
	export class VersionInfo {
	    version: string;
	    label: string;
	    sizeMB: number;
	    default: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.label = source["label"];
	        this.sizeMB = source["sizeMB"];
	        this.default = source["default"];
	    }
	}

}

