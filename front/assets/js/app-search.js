const date = new Date();
var mapping = [];
var filter_operation = ["is", "is_not", "exists", "does_not_exists"]
var filters_set = {}
date.setMinutes(date.getMinutes() - 15)
$.datetimepicker.setLocale('ru');
$('#datetimepicker_start').datetimepicker({timepicker: true, format:'Y-m-d H:i:s', step: 15, value:date});
$('#datetimepicker_end').datetimepicker({
  timepicker: true, 
  format:'Y-m-d H:i:s', 
  step: 15,
  value:new Date(),
  onShow:function( ct ){
   this.setOptions({
    minDate:$('#datetimepicker_start').val()?$('#datetimepicker_start').val():false
   })
  }
});
//var getnodes = setInterval(NodeStatus, 5000);
//var getindices = setInterval(IndexList, 3000);

function bytesToSize(bytes) {
   var sizes = ['b', 'kb', 'mb', 'gb', 'tb'];
   if (bytes == 0) return '0 byte';
   var i = parseInt(Math.floor(Math.log(bytes) / Math.log(1024)));
   return Math.round(bytes / Math.pow(1024, i), 2) + ' ' + sizes[i];
}

$(document).ready(function(){
    var post = {
      "action": "get_clusters"
    };

    $.ajax({
      type: "POST",
      url: "/api/",
      data: JSON.stringify(post),
      dataType: 'json',
      contentType: 'application/json',
      success: function (data) {
        var str = "";
        for(var k in data) {
          name = data[k].Name;
          host = data[k].Host;
          $('#clusters').append(new Option(name, host,false,false));
        }
    }
  });
});

$('#clusters').on('change', function(e) {
    var cluster = this.value;
    if (cluster!=-1) {
      var post = {
        "action": "get_index_groups",
        "search" : {
          "cluster": cluster
        }
      };
      $('#cluster').val(cluster);
      $('#igs').find('option')
      .remove()
      .end();
      $.ajax({
        type: "POST",
        url: "/api/",
        data: JSON.stringify(post),
        dataType: 'json',
        contentType: 'application/json',
        success: function (data) {
          var str = "";
          $('#igs').append(new Option("Select...", "",true,true));
          for(var k in data) {
            index = data[k].index;
            $('#igs').append(new Option(index, index,false,false));
          }
      }
    });
  }
});

$('#igs').on('change', function(e) {
      var post = {
        "action": "get_mapping",
        "search" : {
          "cluster": $('#cluster').val(),
          "index": $('#igs').val()
        }
      };
      $.ajax({
        type: "POST",
        url: "/api/",
        data: JSON.stringify(post),
        dataType: 'json',
        contentType: 'application/json',
        success: function (data) {
          var str = "";
          mapping = [];
          for (var k in data) {
            str += "<li class='list-group-item' style='word-wrap: break-word !important; word-break: break-word;'><input type='checkbox' name='fields' id='mapping"+k+"' data-type='" + data[k] + "' value='" + k + "'>&nbsp;<label for='mapping"+k+"'>"+ k + "</label>&nbsp;&nbsp;(" + data[k] + ")" +"</li>";
            mapping.push(k);
          }
          $("#fields").html(str);
      }
    });
});

$('#modal_add_filter').on('shown.bs.modal',function(e){

    $('#add_filter_fieldlist').find('option')
    .remove()
    .end();

    $('#add_filter_operation').find('option')
    .remove()
    .end();

    $('#add_filter_uuid').val((Math.random() + 1).toString(36).substring(7));
    
    $('#add_filter_fieldlist').append(new Option("Select...", "",true,true));
    for (k in mapping) {
          optText = mapping[k];
          optValue = mapping[k];
          $('#add_filter_fieldlist').append(new Option(optText, optValue,false,false));
    }
    
    $('#add_filter_operation').append(new Option("Select...", "",true,true));
    for (k in filter_operation) {
          optText = filter_operation[k];
          optValue = filter_operation[k];
          $('#add_filter_operation').append(new Option(optText, optValue,false,false));
    }

});

$("#add_filter_save").click(function(){
  var str = $("#filters").html();
  var btn_id = $("#add_filter_uuid").val()
  if ($("#add_filter_operation").val()=="is") {
    str += "<button type='button' id='" + $("#add_filter_uuid").val() + "' class='btn filter' data-target='#modal_update_filter' data-toggle='modal' data-uuid='" + $("#add_filter_uuid").val() + "' data-field='" + $("#add_filter_fieldlist").val() + "' data-oper='is' data-value='"+$("#add_filter_value").val()+"'>" + $("#add_filter_fieldlist").val() + ":" + $("#add_filter_value").val() + "</button>";
  } else if ($("#add_filter_operation").val()=="is_not") {
    str += "<button type='button' id='" + $("#add_filter_uuid").val() + "' class='btn filter' data-target='#modal_update_filter' data-toggle='modal' data-uuid='" + $("#add_filter_uuid").val() + "' data-field='" + $("#add_filter_fieldlist").val() + "' data-oper='is_not' data-value='"+$("#add_filter_value").val()+"'> NOT " + $("#add_filter_fieldlist").val() + ":" + $("#add_filter_value").val() + "</button>";
  } else if ($("#add_filter_operation").val()=="exists") {
    str += "<button type='button' id='" + $("#add_filter_uuid").val() + "' class='btn filter' data-target='#modal_update_filter' data-toggle='modal' data-uuid='" + $("#add_filter_uuid").val() + "' data-field='" + $("#add_filter_fieldlist").val() + "' data-oper='exists' data-value=''>" + $("#add_filter_fieldlist").val() + ": exists</button>";
  } else if ($("#add_filter_operation").val()=="does_not_exists") {
    str += "<button type='button' id='" + $("#add_filter_uuid").val() + "' class='btn filter' data-target='#modal_update_filter' data-toggle='modal' data-uuid='" + $("#add_filter_uuid").val() + "' data-field='" + $("#add_filter_fieldlist").val() + "' data-oper='does_not_exists' data-value=''>" + $("#add_filter_fieldlist").val() + ": not exists</button>";
  }
  $("#filters").html(str);
  filters_set[btn_id] = {"field":$("#add_filter_fieldlist").val(), "operation": $("#add_filter_operation").val(), "value": $("#add_filter_value").val()};
  event.preventDefault();
});

$("#update_filter").click(function(){
  var btn_id = $("#update_filter_uuid").val()
  if ($("#update_filter_operation").val()=="is") {
    $("#"+btn_id).html($("#update_filter_fieldlist").val() + ":" + $("#update_filter_value").val())
    $("#"+btn_id).attr("data-value", $("#update_filter_value").val())
    $("#"+btn_id).attr("data-field", $("#update_filter_fieldlist").val())
    $("#"+btn_id).attr("data-oper", "is")
  } else if ($("#update_filter_operation").val()=="is_not") {
    $("#"+btn_id).html($("#update_filter_fieldlist").val() + ": not " + $("#update_filter_value").val())
    $("#"+btn_id).attr("data-value", $("#update_filter_value").val())
    $("#"+btn_id).attr("data-field", $("#update_filter_fieldlist").val())
    $("#"+btn_id).attr("data-oper", "is_not")
  } else if ($("#update_filter_operation").val()=="exists") {
    $("#"+btn_id).html($("#update_filter_fieldlist").val() + ": exists")
    $("#"+btn_id).attr("data-field", $("#update_filter_fieldlist").val())
    $("#"+btn_id).attr("data-value", "")
    $("#"+btn_id).attr("data-oper", "exists")
  } else if ($("#update_filter_operation").val()=="does_not_exists") {
    $("#"+btn_id).html($("#update_filter_fieldlist").val() + ": does_not_exists")
    $("#"+btn_id).attr("data-field", $("#update_filter_fieldlist").val())
    $("#"+btn_id).attr("data-value", "")
    $("#"+btn_id).attr("data-oper", "does_not_exists")
  }

  filters_set[btn_id] = {"field":$("#update_filter_fieldlist").val(), "operation": $("#update_filter_operation").val(), "value": $("#update_filter_value").val()};
  event.preventDefault();
});

$("#remove_filter").click(function(e){
  var btn_id = $("#update_filter_uuid").val()
  $("#"+btn_id).remove()
  delete filters_set[btn_id]
});

$("#filters").on("click", "button.filter", function(e){
    filter_btn = $(e.target)
    $('#update_filter_uuid').val(filter_btn.attr("data-uuid"))
    $('#update_filter_value').val(filter_btn.attr("data-value"))

    $('#update_filter_fieldlist').find('option')
    .remove()
    .end();

    $('#update_filter_operation').find('option')
    .remove()
    .end();

    for (k in mapping) {
      optText = mapping[k];
      optValue = mapping[k];
      if (optValue==filter_btn.attr("data-field")) {
        $('#update_filter_fieldlist').append(new Option(optText, optValue,true,true));
      } else {
        $('#update_filter_fieldlist').append(new Option(optText, optValue,false,false));
      }
    }

    for (k in filter_operation) {
          optText = filter_operation[k];
          optValue = filter_operation[k];
          if (optValue==filter_btn.attr("data-oper")) {
            $('#update_filter_operation').append(new Option(optText, optValue,true,true));
          } else {
            $('#update_filter_operation').append(new Option(optText, optValue,false,false));
          }
    }
});

$("#search").click(function(){
    $("#loading").removeClass('invisible');
    $("#result").html("");
    fields = [];
    tf = [];
    xql = $('#xql').val();
    indexOfLargestValue = 0;
    total = 0;
    $("input[name='fields']").each(function() {
      if (this.dataset.type=="date") {
        tf.push(this.value)
      }
      if (this.checked) {
        fields.push(this.value)
      }
    });

    var post = {
      "action": "search",
      "search" : {
        "index": $('#igs').val(),
        "fields": fields,
        "filters": filters_set,
        "timefields": tf,
        "cluster": $('#cluster').val(),
        "xql": xql,
        "date_start":$('#datetimepicker_start').val(),
        "date_end":$('#datetimepicker_end').val(),
        "count": true
      }
    };

    $.ajax({
      type: "POST",
      url: "/api/",
      data: JSON.stringify(post),
      dataType: 'json',
      contentType: 'application/json',
      success: function (data) {
        total = data.count
      } 
    });

    var post = {
      "action": "search",
      "search" : {
        "index": $('#igs').val(),
        "fields": fields,
        "filters": filters_set,
        "timefields": tf,
        "cluster": $('#cluster').val(),
        "xql": xql,
        "date_start":$('#datetimepicker_start').val(),
        "date_end":$('#datetimepicker_end').val(),
        "count": false
      }
    };

    $.ajax({
      type: "POST",
      url: "/api/",
      data: JSON.stringify(post),
      dataType: 'json',
      contentType: 'application/json',
      success: function (data) {
        res = data.hits.hits
        
        var str = "";

        if ( res.length > 0 ) {
          if ( fields.length > 0 ) {
            fields_size = []
            for (var k in res) {
              fields_size[k] = Object.keys(res[k].fields).length
            }
            indexOfLargestValue = fields_size.reduce((maxIndex, currentValue, currentIndex, array) => currentValue > array[maxIndex] ? currentIndex : maxIndex, 0)
          }
           str += "<h3>Это демо-версия результатов поиска - "+res.length+" из "+total+" записей</h3>"
           str += "<table  class='table'><thead><tr><th>Time (desc)</th>";
            if ( fields.length == 0 ) {
              str+="<th>_source</th>";
            } else {
              for (var f in res[indexOfLargestValue].fields) {
                  if (f==tf[0]) {
                    continue;
                  } else {
                    str+="<th>" + f + "</th>";
                  }
              }
            }
          str+="</tr></thead><tbody>";
          if (xql !="") {
            term = xql.replace(new RegExp(" ", "gi"), (match) => `|`);
          }
          for (var k in res) {
            str += "<tr>";
            if ( fields.length == 0 ) {
              r = JSON.stringify(res[k]._source)
              if (xql !="") {
                s = r.replace(new RegExp(term, "gi"), (match) => `<mark>${match}</mark>`);
              } else {
                s = r
              }
              str += "<td>"+ res[k]._source[tf[0]] +"</td>";
              str += "<td style='word-wrap: break-word !important; word-break: break-word;'>"+ s +"</td>";
            } else {
              str += "<td>"+ res[k].fields[tf[0]] +"</td>";
              for (var f in res[indexOfLargestValue].fields) {
                if (f==tf[0]) {
                   continue;
                } else {
                  if(typeof res[k].fields[f] === "undefined"){
                    r = " --- "
                  } else {
                    r = res[k].fields[f].toString();
                  }
                  if (xql !="") {
                    s = r.replace(new RegExp(term, "gi"), (match) => `<mark>${match}</mark>`);
                  } else {
                    s = r
                  }
                  str += "<td style='word-wrap: break-word !important; word-break: break-word;'>" + s + "</td>";
                }
              }
            }
            str+="</tr>";
          }
          str+="</tbody></table>";
        $("#xtract_csv").prop('disabled', false);
        } else {
          str = "<h3>No search results found</h3>";
          $("#xtract_csv").prop('disabled', true);
        }
        $("#result").html(str);
      },
      error: function (data) {
        $("#result").html(data);
      }
    });
    $("#loading").addClass('invisible');
    //event.preventDefault();
});

$("#xtract_csv").click(function(){
    $("#loading").removeClass('invisible');
    fname = (Math.random() + 1).toString(36).substring(4);
    fields = []
    tf = []
    xql = $('#xql').val()
    indexOfLargestValue = 0
    $("input[name='fields']").each(function() {
      if (this.dataset.type=="date") {
        tf.push(this.value)
      }
      if (this.checked) {
        fields.push(this.value)
      }
    });

    var post = {
      "action": "prepare_csv",
      "search" : {
        "index": $('#igs').val(),
        "fields": fields,
        "mapping": mapping,
        "filters": filters_set,
        "timefields": tf,
        "cluster": $('#cluster').val(),
        "xql": xql,
        "date_start":$('#datetimepicker_start').val(),
        "date_end":$('#datetimepicker_end').val(),
        "count": false,
        "fname": fname
      }
    };

    $.ajax({
      type: "POST",
      url: "/api/",
      data: JSON.stringify(post),
      dataType: 'json',
      contentType: 'application/json',
      success: function (data) {},
      error: function (data) {
        $("#download_link").html("<a href='/data/"+fname+".csv'>скачать</a>");
        document.location.href = "/data/"+fname+".csv";
      }
    })
    $("#loading").addClass('invisible');
});

$('#add_filter').on('hidden.bs.modal',function(){
	$('#add_filter_form').trigger('reset');
});