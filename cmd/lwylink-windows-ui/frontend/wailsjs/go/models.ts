export namespace main {
	
	export class UpdateState {
	    status: string;
	    message: string;
	    currentVersion: string;
	    latestVersion: string;
	    platform: string;
	    notes: string;
	    mandatory: boolean;
	    installArgs: string;
	    fileName: string;
	    sizeBytes: number;
	    sha256: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.platform = source["platform"];
	        this.notes = source["notes"];
	        this.mandatory = source["mandatory"];
	        this.installArgs = source["installArgs"];
	        this.fileName = source["fileName"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sha256 = source["sha256"];
	    }
	}
	export class ConnectionState {
	    status: string;
	    message: string;
	    roomID: number;
	    ip: string;
	    directPeerIPs: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.roomID = source["roomID"];
	        this.ip = source["ip"];
	        this.directPeerIPs = source["directPeerIPs"];
	    }
	}
	export class ViewVoiceParticipant {
	    roomID: number;
	    userID: number;
	    username: string;
	    displayName: string;
	    speaking: boolean;
	    muted: boolean;
	    joinedAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewVoiceParticipant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.userID = source["userID"];
	        this.username = source["username"];
	        this.displayName = source["displayName"];
	        this.speaking = source["speaking"];
	        this.muted = source["muted"];
	        this.joinedAt = source["joinedAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ViewVoiceState {
	    roomID: number;
	    maxMembers: number;
	    maxSpeakers: number;
	    participantCount: number;
	    speakerCount: number;
	    participants: ViewVoiceParticipant[];
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewVoiceState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.maxMembers = source["maxMembers"];
	        this.maxSpeakers = source["maxSpeakers"];
	        this.participantCount = source["participantCount"];
	        this.speakerCount = source["speakerCount"];
	        this.participants = this.convertValues(source["participants"], ViewVoiceParticipant);
	        this.updatedAt = source["updatedAt"];
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
	export class ViewChatMessage {
	    id: string;
	    roomID: number;
	    userID: number;
	    username: string;
	    displayName: string;
	    message: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.roomID = source["roomID"];
	        this.userID = source["userID"];
	        this.username = source["username"];
	        this.displayName = source["displayName"];
	        this.message = source["message"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ViewMember {
	    roomID: number;
	    userID: number;
	    username: string;
	    displayName: string;
	    role: string;
	    online: boolean;
	    virtualIP?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.userID = source["userID"];
	        this.username = source["username"];
	        this.displayName = source["displayName"];
	        this.role = source["role"];
	        this.online = source["online"];
	        this.virtualIP = source["virtualIP"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ViewRoom {
	    id: number;
	    name: string;
	    ownerUserID: number;
	    nodeID: number;
	    roomPublicKey: string;
	    networkCIDR: string;
	    maxMembers: number;
	    voiceMaxMembers: number;
	    voiceMaxSpeakers: number;
	    memberCount: number;
	    status: string;
	    nodeName?: string;
	    nodeOverlayAddr?: string;
	    nodeUDPAddr?: string;
	    nodePublicKey?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewRoom(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.ownerUserID = source["ownerUserID"];
	        this.nodeID = source["nodeID"];
	        this.roomPublicKey = source["roomPublicKey"];
	        this.networkCIDR = source["networkCIDR"];
	        this.maxMembers = source["maxMembers"];
	        this.voiceMaxMembers = source["voiceMaxMembers"];
	        this.voiceMaxSpeakers = source["voiceMaxSpeakers"];
	        this.memberCount = source["memberCount"];
	        this.status = source["status"];
	        this.nodeName = source["nodeName"];
	        this.nodeOverlayAddr = source["nodeOverlayAddr"];
	        this.nodeUDPAddr = source["nodeUDPAddr"];
	        this.nodePublicKey = source["nodePublicKey"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ViewUser {
	    id: number;
	    username: string;
	    email: string;
	    displayName: string;
	    role: string;
	    membership: string;
	    disabled: boolean;
	    relayTrafficUsedBytes: number;
	    relayTrafficExtraBytes: number;
	    relayTrafficQuotaBytes: number;
	    relayTrafficRemainingBytes: number;
	    relayTrafficPeriod: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.email = source["email"];
	        this.displayName = source["displayName"];
	        this.role = source["role"];
	        this.membership = source["membership"];
	        this.disabled = source["disabled"];
	        this.relayTrafficUsedBytes = source["relayTrafficUsedBytes"];
	        this.relayTrafficExtraBytes = source["relayTrafficExtraBytes"];
	        this.relayTrafficQuotaBytes = source["relayTrafficQuotaBytes"];
	        this.relayTrafficRemainingBytes = source["relayTrafficRemainingBytes"];
	        this.relayTrafficPeriod = source["relayTrafficPeriod"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class VoicePreferences {
	    inputDeviceID: string;
	    outputDeviceID: string;
	    micVolume: number;
	    outputVolume: number;
	    autoGainControl: boolean;
	    noiseSuppression: boolean;
	    echoCancellation: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VoicePreferences(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputDeviceID = source["inputDeviceID"];
	        this.outputDeviceID = source["outputDeviceID"];
	        this.micVolume = source["micVolume"];
	        this.outputVolume = source["outputVolume"];
	        this.autoGainControl = source["autoGainControl"];
	        this.noiseSuppression = source["noiseSuppression"];
	        this.echoCancellation = source["echoCancellation"];
	    }
	}
	export class AppState {
	    apiBaseURL: string;
	    interfaceName: string;
	    p2pEnabled: boolean;
	    voiceSettings: VoicePreferences;
	    user?: ViewUser;
	    rooms: ViewRoom[];
	    currentRoom?: ViewRoom;
	    members: ViewMember[];
	    chatMessages: ViewChatMessage[];
	    voice: ViewVoiceState;
	    voiceMode: string;
	    voiceMessage: string;
	    connection: ConnectionState;
	    update: UpdateState;
	    announcement: string;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiBaseURL = source["apiBaseURL"];
	        this.interfaceName = source["interfaceName"];
	        this.p2pEnabled = source["p2pEnabled"];
	        this.voiceSettings = this.convertValues(source["voiceSettings"], VoicePreferences);
	        this.user = this.convertValues(source["user"], ViewUser);
	        this.rooms = this.convertValues(source["rooms"], ViewRoom);
	        this.currentRoom = this.convertValues(source["currentRoom"], ViewRoom);
	        this.members = this.convertValues(source["members"], ViewMember);
	        this.chatMessages = this.convertValues(source["chatMessages"], ViewChatMessage);
	        this.voice = this.convertValues(source["voice"], ViewVoiceState);
	        this.voiceMode = source["voiceMode"];
	        this.voiceMessage = source["voiceMessage"];
	        this.connection = this.convertValues(source["connection"], ConnectionState);
	        this.update = this.convertValues(source["update"], UpdateState);
	        this.announcement = source["announcement"];
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
	export class ChangePasswordInput {
	    oldPassword: string;
	    newPassword: string;
	
	    static createFrom(source: any = {}) {
	        return new ChangePasswordInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldPassword = source["oldPassword"];
	        this.newPassword = source["newPassword"];
	    }
	}
	
	export class CreateRoomInput {
	    name: string;
	    maxMembers: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateRoomInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.maxMembers = source["maxMembers"];
	    }
	}
	export class EmailVerificationInput {
	    email: string;
	    purpose: string;
	
	    static createFrom(source: any = {}) {
	        return new EmailVerificationInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.purpose = source["purpose"];
	    }
	}
	export class JoinRoomInput {
	    roomPublicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new JoinRoomInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomPublicKey = source["roomPublicKey"];
	    }
	}
	export class LoginInput {
	    email: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.password = source["password"];
	    }
	}
	export class MemberActionInput {
	    userID: number;
	
	    static createFrom(source: any = {}) {
	        return new MemberActionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.userID = source["userID"];
	    }
	}
	export class RegisterInput {
	    email: string;
	    password: string;
	    displayName: string;
	    emailCode: string;
	
	    static createFrom(source: any = {}) {
	        return new RegisterInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.password = source["password"];
	        this.displayName = source["displayName"];
	        this.emailCode = source["emailCode"];
	    }
	}
	export class ResetPasswordInput {
	    email: string;
	    emailCode: string;
	    newPassword: string;
	
	    static createFrom(source: any = {}) {
	        return new ResetPasswordInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.emailCode = source["emailCode"];
	        this.newPassword = source["newPassword"];
	    }
	}
	export class SendChatInput {
	    roomID: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new SendChatInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.message = source["message"];
	    }
	}
	export class SettingsInput {
	    apiBaseURL: string;
	    interfaceName: string;
	    p2pEnabled?: boolean;
	    voiceSettings?: VoicePreferences;
	
	    static createFrom(source: any = {}) {
	        return new SettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiBaseURL = source["apiBaseURL"];
	        this.interfaceName = source["interfaceName"];
	        this.p2pEnabled = source["p2pEnabled"];
	        this.voiceSettings = this.convertValues(source["voiceSettings"], VoicePreferences);
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
	
	
	
	
	
	
	
	export class VoiceAudioFrameInput {
	    roomID: number;
	    seq: number;
	    timestampMS: number;
	    codec: string;
	    sampleRate: number;
	    payload: number[];
	
	    static createFrom(source: any = {}) {
	        return new VoiceAudioFrameInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.seq = source["seq"];
	        this.timestampMS = source["timestampMS"];
	        this.codec = source["codec"];
	        this.sampleRate = source["sampleRate"];
	        this.payload = source["payload"];
	    }
	}
	
	export class VoiceWebRTCSignalInput {
	    roomID: number;
	    toUserID: number;
	    type: string;
	    payload: number[];
	
	    static createFrom(source: any = {}) {
	        return new VoiceWebRTCSignalInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.toUserID = source["toUserID"];
	        this.type = source["type"];
	        this.payload = source["payload"];
	    }
	}

}

