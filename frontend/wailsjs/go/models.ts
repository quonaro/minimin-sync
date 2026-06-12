export namespace config {
	
	export class Server {
	    id: string;
	    name: string;
	    token: string;
	    baseUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.token = source["token"];
	        this.baseUrl = source["baseUrl"];
	    }
	}
	export class Config {
	    instancesDir: string;
	    servers: Server[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instancesDir = source["instancesDir"];
	        this.servers = this.convertValues(source["servers"], Server);
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

}

export namespace instance {
	
	export class Marker {
	    serverId: string;
	    token: string;
	    baseUrl: string;
	    lastSyncAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Marker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.token = source["token"];
	        this.baseUrl = source["baseUrl"];
	        this.lastSyncAt = source["lastSyncAt"];
	    }
	}
	export class ScannedInstance {
	    Dir: string;
	    Name: string;
	    Marker?: Marker;
	
	    static createFrom(source: any = {}) {
	        return new ScannedInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Dir = source["Dir"];
	        this.Name = source["Name"];
	        this.Marker = this.convertValues(source["Marker"], Marker);
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

}

