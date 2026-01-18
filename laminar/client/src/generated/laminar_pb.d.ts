import * as jspb from 'google-protobuf'

import * as google_protobuf_struct_pb from 'google-protobuf/google/protobuf/struct_pb'; // proto import: "google/protobuf/struct.proto"


export class RequestToBalancer extends jspb.Message {
  getQueryid(): string;
  setQueryid(value: string): RequestToBalancer;

  getQuerysql(): string;
  setQuerysql(value: string): RequestToBalancer;

  getPayload(): Uint8Array | string;
  getPayload_asU8(): Uint8Array;
  getPayload_asB64(): string;
  setPayload(value: Uint8Array | string): RequestToBalancer;

  getUrlcallback(): string;
  setUrlcallback(value: string): RequestToBalancer;

  getAction(): string;
  setAction(value: string): RequestToBalancer;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): RequestToBalancer.AsObject;
  static toObject(includeInstance: boolean, msg: RequestToBalancer): RequestToBalancer.AsObject;
  static serializeBinaryToWriter(message: RequestToBalancer, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): RequestToBalancer;
  static deserializeBinaryFromReader(message: RequestToBalancer, reader: jspb.BinaryReader): RequestToBalancer;
}

export namespace RequestToBalancer {
  export type AsObject = {
    queryid: string,
    querysql: string,
    payload: Uint8Array | string,
    urlcallback: string,
    action: string,
  }
}

export class ResponseToBalancer extends jspb.Message {
  getStatus(): string;
  setStatus(value: string): ResponseToBalancer;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): ResponseToBalancer.AsObject;
  static toObject(includeInstance: boolean, msg: ResponseToBalancer): ResponseToBalancer.AsObject;
  static serializeBinaryToWriter(message: ResponseToBalancer, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): ResponseToBalancer;
  static deserializeBinaryFromReader(message: ResponseToBalancer, reader: jspb.BinaryReader): ResponseToBalancer;
}

export namespace ResponseToBalancer {
  export type AsObject = {
    status: string,
  }
}

export class CallBackRequest extends jspb.Message {
  getQueryid(): string;
  setQueryid(value: string): CallBackRequest;

  getQuerysql(): string;
  setQuerysql(value: string): CallBackRequest;

  getPayload(): Uint8Array | string;
  getPayload_asU8(): Uint8Array;
  getPayload_asB64(): string;
  setPayload(value: Uint8Array | string): CallBackRequest;

  getUrlcallback(): string;
  setUrlcallback(value: string): CallBackRequest;

  getAction(): string;
  setAction(value: string): CallBackRequest;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): CallBackRequest.AsObject;
  static toObject(includeInstance: boolean, msg: CallBackRequest): CallBackRequest.AsObject;
  static serializeBinaryToWriter(message: CallBackRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): CallBackRequest;
  static deserializeBinaryFromReader(message: CallBackRequest, reader: jspb.BinaryReader): CallBackRequest;
}

export namespace CallBackRequest {
  export type AsObject = {
    queryid: string,
    querysql: string,
    payload: Uint8Array | string,
    urlcallback: string,
    action: string,
  }
}

export class CallBackResponse extends jspb.Message {
  getQueryid(): string;
  setQueryid(value: string): CallBackResponse;

  getQuerysql(): string;
  setQuerysql(value: string): CallBackResponse;

  getRecordsList(): Array<google_protobuf_struct_pb.Struct>;
  setRecordsList(value: Array<google_protobuf_struct_pb.Struct>): CallBackResponse;
  clearRecordsList(): CallBackResponse;
  addRecords(value?: google_protobuf_struct_pb.Struct, index?: number): google_protobuf_struct_pb.Struct;

  getUrlcallback(): string;
  setUrlcallback(value: string): CallBackResponse;

  getAction(): string;
  setAction(value: string): CallBackResponse;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): CallBackResponse.AsObject;
  static toObject(includeInstance: boolean, msg: CallBackResponse): CallBackResponse.AsObject;
  static serializeBinaryToWriter(message: CallBackResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): CallBackResponse;
  static deserializeBinaryFromReader(message: CallBackResponse, reader: jspb.BinaryReader): CallBackResponse;
}

export namespace CallBackResponse {
  export type AsObject = {
    queryid: string,
    querysql: string,
    recordsList: Array<google_protobuf_struct_pb.Struct.AsObject>,
    urlcallback: string,
    action: string,
  }
}

export class PingRequest extends jspb.Message {
  getMessage(): string;
  setMessage(value: string): PingRequest;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): PingRequest.AsObject;
  static toObject(includeInstance: boolean, msg: PingRequest): PingRequest.AsObject;
  static serializeBinaryToWriter(message: PingRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): PingRequest;
  static deserializeBinaryFromReader(message: PingRequest, reader: jspb.BinaryReader): PingRequest;
}

export namespace PingRequest {
  export type AsObject = {
    message: string,
  }
}

export class PingResponse extends jspb.Message {
  getMessage(): string;
  setMessage(value: string): PingResponse;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): PingResponse.AsObject;
  static toObject(includeInstance: boolean, msg: PingResponse): PingResponse.AsObject;
  static serializeBinaryToWriter(message: PingResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): PingResponse;
  static deserializeBinaryFromReader(message: PingResponse, reader: jspb.BinaryReader): PingResponse;
}

export namespace PingResponse {
  export type AsObject = {
    message: string,
  }
}

