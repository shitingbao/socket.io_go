package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zishang520/engine.io/events"
	"github.com/zishang520/engine.io/log"
	"github.com/zishang520/engine.io/types"
)

var parser_log = log.NewLog("socket.io:parser")

// A socket.io Decoder instance
type decoder struct {
	events.EventEmitter

	reconstructor *binaryreconstructor
}

func NewDecoder() Decoder {
	return &decoder{EventEmitter: events.New()}
}

// Decodes an encoded packet string into packet JSON.
func (d *decoder) Add(data interface{}) error {
	switch tdata := data.(type) {
	case string:
		if d.reconstructor != nil {
			return errors.New("got plaintext data when reconstructing a packet")
		}
		if err := d.decodeAsString(types.NewStringBufferString(tdata)); err != nil {
			return err
		}
	case *strings.Reader:
		if d.reconstructor != nil {
			return errors.New("got plaintext data when reconstructing a packet")
		}
		rdata, err := types.NewStringBufferReader(tdata)
		if err != nil {
			return err
		}
		if err := d.decodeAsString(rdata); err != nil {
			return err
		}
	case *types.StringBuffer:
		if d.reconstructor != nil {
			return errors.New("got plaintext data when reconstructing a packet")
		}
		if err := d.decodeAsString(tdata); err != nil {
			return err
		}
	default:
		if IsBinary(data) {
			// raw binary data
			if d.reconstructor == nil {
				return errors.New("got binary data when not reconstructing a packet")
			}

			rdata := types.NewBytesBuffer(nil)
			switch tdata := data.(type) {
			case io.Reader:
				if c, ok := data.(io.Closer); ok {
					defer c.Close()
				}
				if _, err := rdata.ReadFrom(tdata); err != nil {
					return err
				}
			case []byte:
				if _, err := rdata.Write(tdata); err != nil {
					return err
				}
			}
			packet, err := d.reconstructor.takeBinaryData(rdata)
			if err != nil {
				return errors.New(fmt.Sprintf("Decode error: %v", err.Error()))
			}
			if packet != nil {
				// received final buffer
				d.reconstructor = nil
				d.Emit("decoded", packet)
			}
		} else {
			return errors.New(fmt.Sprintf("Unknown type: %v", data))
		}
	}

	return nil
}

func (d *decoder) decodeAsString(str types.BufferInterface) error {
	packet, err := d.decodeString(str)
	if err != nil {
		parser_log.Debug("decode err %v", err)
		return err
	}
	if packet.Type == BINARY_EVENT || packet.Type == BINARY_ACK {
		// binary packet's json
		d.reconstructor = NewBinaryReconstructor(packet)
		// no attachments, labeled binary but no binary data to follow
		if packet.Attachments == 0 {
			d.Emit("decoded", packet)
		}
	} else {
		// non-binary full packet
		d.Emit("decoded", packet)
	}
	return nil
}

// Decode a packet String (JSON data)
func (d *decoder) decodeString(str types.BufferInterface) (packet *Packet, err error) {
	defer func(str string) {
		if err == nil {
			parser_log.Debug("decoded %s as %v", str, packet)
		}
	}(str.String())

	// look up type
	packet = &Packet{}
	msgType, err := str.ReadByte()
	if err != nil {
		return nil, errors.New("invalid payload")
	}
	packet.Type = PacketType(msgType)
	if !packet.Type.Valid() {
		return nil, errors.New(fmt.Sprintf("unknown packet type %d", packet.Type))
	}
	// look up attachments if type binary
	if packet.Type == BINARY_EVENT || packet.Type == BINARY_ACK {
		buf, err := str.ReadString('-')
		if err != nil {
			// The scan is over and it is not found '-' indicating that there is a problem.
			return nil, errors.New("Illegal attachments")
		}
		_l := len(buf)
		if _l < 2 { // 'xxx-'
			return nil, errors.New("Illegal attachments")
		}
		attachments, err := strconv.ParseUint(buf[:_l-1], 10, 64)
		if err != nil {
			return nil, errors.New("Illegal attachments")
		}
		packet.Attachments = attachments
	}

	// look up namespace (if any)
	if nsp, err := str.ReadByte(); err == nil {
		if '/' == nsp {
			_nsp, err := str.ReadString(',')
			if err != nil {
				if err == io.EOF {
					packet.Nsp = "/" + _nsp
				}
				return nil, errors.New("Illegal namespace")
			} else {
				_l := len(_nsp)
				if _l < 1 {
					return nil, errors.New("Illegal namespace")
				}
				packet.Nsp = "/" + _nsp[:_l-1]
			}
		} else {
			packet.Nsp = "/"
			if err := str.UnreadByte(); err != nil {
				return nil, errors.New("Illegal namespace")
			}
		}
	} else {
		if err != io.EOF {
			return nil, errors.New("Illegal namespace")
		} else {
			packet.Nsp = "/"
		}
	}

	if str.Len() > 0 {
		// look up id
		id := new(strings.Builder)

		for {
			b, err := str.ReadByte()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			if '0' <= b && '9' >= b {
				if err := id.WriteByte(b); err != nil {
					return nil, err
				}
			} else {
				if err := str.UnreadByte(); err != nil {
					return nil, errors.New("Illegal id")
				}
				break
			}
		}

		if id.Len() > 0 {
			id, err := strconv.ParseUint(id.String(), 10, 64)
			if err != nil {
				return nil, err
			}
			packet.Id = id
		}
	}

	// look up json data
	if str.Len() > 0 {
		var payload interface{}
		if json.NewDecoder(str).Decode(&payload) != nil {
			return nil, errors.New("invalid payload")
		}
		if isPayloadValid(packet.Type, payload) {
			packet.Data = payload
		} else {
			return nil, errors.New("invalid payload")
		}
	}

	return packet, nil
}

func isPayloadValid(t PacketType, payload interface{}) bool {
	switch t {
	case CONNECT:
		_, ok := payload.(map[string]interface{})
		return ok
	case DISCONNECT:
		return payload == nil
	case CONNECT_ERROR:
		_, ok := payload.(map[string]interface{})
		if !ok {
			_, ok = payload.(string)
		}
		return ok
	case EVENT, BINARY_EVENT:
		data, ok := payload.([]interface{})
		return ok && len(data) > 0
	case ACK, BINARY_ACK:
		_, ok := payload.([]interface{})
		return ok
	}
	return false
}

// Deallocates a parser's resources
func (d *decoder) Destroy() {
	if d.reconstructor != nil {
		d.reconstructor.finishedReconstruction()
	}
}
